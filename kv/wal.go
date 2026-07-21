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
	walMagic         = "MYWAL01\x00"
	walHeader        = 8 + 1 + 4 + 4 + 4 // magic, op, key length, value length, checksum
	walPut      byte = 1
	walDelete   byte = 2
	walTxPut    byte = 3
	walTxDelete byte = 4
	walTxCommit byte = 5
)

type wal struct{ file *os.File }

// BatchOp describes one durable transaction operation.
type BatchOp struct {
	Key, Value []byte
	Delete     bool
}

func (w *wal) size() (int64, error) {
	info, err := w.file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat WAL: %w", err)
	}
	return info.Size(), nil
}

func openWAL(path string) (*wal, error) {
	f, err := os.OpenFile(path+".wal", os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening WAL: %w", err)
	}
	return &wal{file: f}, nil
}

func (w *wal) append(op byte, key, value []byte) error {
	if uint64(len(key))+uint64(len(value)) > uint64(^uint32(0)) {
		return fmt.Errorf("WAL record too large")
	}
	payload := append(append([]byte{op}, key...), value...)
	record := make([]byte, walHeader+len(key)+len(value))
	copy(record, walMagic)
	record[8] = op
	binary.LittleEndian.PutUint32(record[9:13], uint32(len(key)))
	binary.LittleEndian.PutUint32(record[13:17], uint32(len(value)))
	binary.LittleEndian.PutUint32(record[17:21], crc32.ChecksumIEEE(payload))
	copy(record[21:], key)
	copy(record[21+len(key):], value)
	if _, err := w.file.Write(record); err != nil {
		return fmt.Errorf("writing WAL: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("syncing WAL: %w", err)
	}
	return nil
}

func (w *wal) appendTransaction(id uint64, ops []BatchOp) error {
	var idbuf [8]byte
	binary.LittleEndian.PutUint64(idbuf[:], id)
	for _, op := range ops {
		key := append(append([]byte{}, idbuf[:]...), op.Key...)
		kind := walTxPut
		if op.Delete {
			kind = walTxDelete
		}
		if err := w.append(kind, key, op.Value); err != nil {
			return err
		}
	}
	return w.append(walTxCommit, idbuf[:], nil)
}

func (w *wal) replay(apply func(byte, []byte, []byte) error) error {
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	info, err := w.file.Stat()
	if err != nil {
		return fmt.Errorf("stat WAL for replay: %w", err)
	}
	remaining := info.Size()
	r := bufio.NewReader(w.file)
	pending := make(map[uint64][]BatchOp)
	for {
		h := make([]byte, walHeader)
		_, err := io.ReadFull(r, h)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		} // incomplete tail: crash during append
		if err != nil {
			return err
		}
		if string(h[:8]) != walMagic {
			return fmt.Errorf("invalid WAL magic")
		}
		remaining -= walHeader
		keyLen, valLen := binary.LittleEndian.Uint32(h[9:13]), binary.LittleEndian.Uint32(h[13:17])
		bodyLen := uint64(keyLen) + uint64(valLen)
		if bodyLen > uint64(remaining) || bodyLen > uint64(^uint(0)>>1) {
			return fmt.Errorf("invalid WAL record length")
		}
		body := make([]byte, int(bodyLen))
		if _, err := io.ReadFull(r, body); err != nil {
			return nil
		}
		remaining -= int64(bodyLen)
		payload := append([]byte{h[8]}, body...)
		if crc32.ChecksumIEEE(payload) != binary.LittleEndian.Uint32(h[17:21]) {
			return fmt.Errorf("WAL checksum mismatch")
		}
		if h[8] != walPut && h[8] != walDelete && h[8] != walTxPut && h[8] != walTxDelete && h[8] != walTxCommit {
			return fmt.Errorf("unknown WAL operation %d", h[8])
		}
		if h[8] == walTxPut || h[8] == walTxDelete {
			if len(body[:keyLen]) < 8 {
				return fmt.Errorf("invalid transaction WAL key")
			}
			id := binary.LittleEndian.Uint64(body[:8])
			pending[id] = append(pending[id], BatchOp{Key: append([]byte(nil), body[8:keyLen]...), Value: append([]byte(nil), body[keyLen:]...), Delete: h[8] == walTxDelete})
			continue
		}
		if h[8] == walTxCommit {
			if len(body[:keyLen]) != 8 {
				return fmt.Errorf("invalid transaction commit")
			}
			id := binary.LittleEndian.Uint64(body[:8])
			for _, op := range pending[id] {
				kind := walPut
				if op.Delete {
					kind = walDelete
				}
				if err := apply(kind, op.Key, op.Value); err != nil {
					return err
				}
			}
			delete(pending, id)
			continue
		}
		if err := apply(h[8], body[:keyLen], body[keyLen:]); err != nil {
			return err
		}
	}
}

func (w *wal) clear() error {
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	return w.file.Sync()
}
func (w *wal) close() error { return w.file.Close() }
