package kv

import (
	"os"
	"testing"
)

func FuzzWALReplay(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte("not a WAL"),
		walRecord(walPut, []byte("key"), []byte("value")),
		append(walRecord(walDelete, []byte("key"), nil), 0xff),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		path := t.TempDir() + "/fuzz.db"
		if err := os.WriteFile(path+".wal", data, 0o600); err != nil {
			t.Fatal(err)
		}
		w, err := openWAL(path)
		if err != nil {
			t.Fatal(err)
		}
		defer w.close()
		_ = w.replay(func(byte, []byte, []byte) error { return nil })
	})
}

func walRecord(op byte, key, value []byte) []byte {
	dir, err := os.MkdirTemp("", "mydb-wal-seed-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	path := dir + "/seed.db"
	w, err := openWAL(path)
	if err != nil {
		panic(err)
	}
	defer w.close()
	if err := w.append(op, key, value); err != nil {
		panic(err)
	}
	data, err := os.ReadFile(path + ".wal")
	if err != nil {
		panic(err)
	}
	return data
}
