package kv

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

const (
	walMagic  = "MYWAL01\x00"
	walHeader = 8 + 1 + 4 + 4 + 4 // magic, op, key length, value length, checksum
	walPut byte = 1
	walDelete byte = 2
)

type wal struct { file *os.File }

func openWAL(path string) (*wal, error) {
	f, err := os.OpenFile(path+".wal", os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil { return nil, fmt.Errorf("opening WAL: %w", err) }
	return &wal{file: f}, nil
}

func (w *wal) append(op byte, key, value []byte) error {
	if uint64(len(key))+uint64(len(value)) > uint64(^uint32(0)) { return fmt.Errorf("WAL record too large") }
	payload := append(append([]byte{op}, key...), value...)
	record := make([]byte, walHeader+len(key)+len(value))
	copy(record, walMagic)
	record[8] = op
	binary.LittleEndian.PutUint32(record[9:13], uint32(len(key)))
	binary.LittleEndian.PutUint32(record[13:17], uint32(len(value)))
	binary.LittleEndian.PutUint32(record[17:21], crc32.ChecksumIEEE(payload))
	copy(record[21:], key)
	copy(record[21+len(key):], value)
	if _, err := w.file.Write(record); err != nil { return fmt.Errorf("writing WAL: %w", err) }
	if err := w.file.Sync(); err != nil { return fmt.Errorf("syncing WAL: %w", err) }
	return nil
}

func (w *wal) replay(apply func(byte, []byte, []byte) error) error {
	if _, err := w.file.Seek(0, io.SeekStart); err != nil { return err }
	r := bufio.NewReader(w.file)
	for {
		h := make([]byte, walHeader)
		_, err := io.ReadFull(r, h)
		if err == io.EOF || err == io.ErrUnexpectedEOF { return nil } // incomplete tail: crash during append
		if err != nil { return err }
		if string(h[:8]) != walMagic { return fmt.Errorf("invalid WAL magic") }
		keyLen, valLen := binary.LittleEndian.Uint32(h[9:13]), binary.LittleEndian.Uint32(h[13:17])
		if uint64(keyLen)+uint64(valLen) > uint64(^uint(0)>>1) { return fmt.Errorf("invalid WAL record length") }
		body := make([]byte, int(keyLen)+int(valLen))
		if _, err := io.ReadFull(r, body); err != nil { return nil }
		payload := append([]byte{h[8]}, body...)
		if crc32.ChecksumIEEE(payload) != binary.LittleEndian.Uint32(h[17:21]) { return fmt.Errorf("WAL checksum mismatch") }
		if h[8] != walPut && h[8] != walDelete { return fmt.Errorf("unknown WAL operation %d", h[8]) }
		if err := apply(h[8], body[:keyLen], body[keyLen:]); err != nil { return err }
	}
}

func (w *wal) clear() error {
	if err := w.file.Truncate(0); err != nil { return err }
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil { return err }
	return w.file.Sync()
}
func (w *wal) close() error { return w.file.Close() }
