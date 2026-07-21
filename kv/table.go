package kv

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// ColumnType is the type of a table column.
type ColumnType byte

const (
	IntType ColumnType = iota + 1
	StringType
	BoolType
)

const (
	TypeInt    = IntType
	TypeString = StringType
	TypeBool   = BoolType
)

// Column describes one field in a Schema. Column order is the on-disk order.
type Column struct {
	Name string
	Type ColumnType
}

// Schema describes the columns a Table accepts.
type Schema struct{ Columns []Column }

// Row is a named set of column values. Values must be int, string, or bool.
type Row map[string]any

// Table gives typed row semantics to a Store. The key is the table's primary key.
type Table struct {
	store  *Store
	schema Schema
}

func NewTable(store *Store, schema Schema) (*Table, error) {
	if store == nil {
		return nil, fmt.Errorf("nil store")
	}
	if err := schema.validate(); err != nil {
		return nil, err
	}
	return &Table{store: store, schema: schema}, nil
}

// OpenTable opens a table backed by path using schema. The schema is supplied by
// the caller because schema catalogs are intentionally outside this milestone.
func OpenTable(path string, schema Schema) (*Table, error) {
	s, err := Open(path)
	if err != nil {
		return nil, err
	}
	t, err := NewTable(s, schema)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	return t, nil
}

func (t *Table) Close() error   { return t.store.Close() }
func (t *Table) Schema() Schema { return t.schema }

func (t *Table) Insert(key string, row Row) error {
	b, err := t.encode(row)
	if err != nil {
		return err
	}
	return t.store.Put([]byte(key), b)
}

func (t *Table) Get(key string) (Row, bool, error) {
	b, found, err := t.store.Get([]byte(key))
	if err != nil || !found {
		return nil, found, err
	}
	r, err := t.decode(b)
	return r, true, err
}

func (t *Table) Delete(key string) error { return t.store.Delete([]byte(key)) }

func (s Schema) validate() error {
	seen := map[string]bool{}
	for _, c := range s.Columns {
		if c.Name == "" || seen[c.Name] {
			return fmt.Errorf("invalid or duplicate column %q", c.Name)
		}
		if c.Type < IntType || c.Type > BoolType {
			return fmt.Errorf("invalid type for column %q", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

func (t *Table) encode(row Row) ([]byte, error) {
	if len(row) != len(t.schema.Columns) {
		return nil, fmt.Errorf("row has wrong number of columns")
	}
	var b bytes.Buffer
	for _, c := range t.schema.Columns {
		v, ok := row[c.Name]
		if !ok {
			return nil, fmt.Errorf("missing column %q", c.Name)
		}
		switch c.Type {
		case IntType:
			i, ok := v.(int)
			if !ok {
				return nil, fmt.Errorf("column %q expects int", c.Name)
			}
			_ = binary.Write(&b, binary.LittleEndian, int64(i))
		case StringType:
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("column %q expects string", c.Name)
			}
			if err := binary.Write(&b, binary.LittleEndian, uint32(len(s))); err != nil {
				return nil, err
			}
			b.WriteString(s)
		case BoolType:
			v, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("column %q expects bool", c.Name)
			}
			if v {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
		}
	}
	return b.Bytes(), nil
}

func (t *Table) decode(data []byte) (Row, error) {
	r := Row{}
	p := 0
	for _, c := range t.schema.Columns {
		switch c.Type {
		case IntType:
			if p+8 > len(data) {
				return nil, fmt.Errorf("invalid row data")
			}
			r[c.Name] = int(int64(binary.LittleEndian.Uint64(data[p : p+8])))
			p += 8
		case StringType:
			if p+4 > len(data) {
				return nil, fmt.Errorf("invalid row data")
			}
			n := int(binary.LittleEndian.Uint32(data[p : p+4]))
			p += 4
			if n < 0 || p+n > len(data) {
				return nil, fmt.Errorf("invalid row data")
			}
			r[c.Name] = string(data[p : p+n])
			p += n
		case BoolType:
			if p >= len(data) || data[p] > 1 {
				return nil, fmt.Errorf("invalid row data")
			}
			r[c.Name] = data[p] == 1
			p++
		}
	}
	if p != len(data) {
		return nil, fmt.Errorf("trailing row data")
	}
	return r, nil
}
