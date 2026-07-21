package kv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// ColumnType is the type of a table column.
type ColumnType byte

const (
	IntType ColumnType = iota + 1
	StringType
	BoolType
	FloatType
	BytesType
	TimestampType
)

const (
	TypeInt       = IntType
	TypeString    = StringType
	TypeBool      = BoolType
	TypeFloat     = FloatType
	TypeBytes     = BytesType
	TypeTimestamp = TimestampType
)

// Column describes one field in a Schema. Column order is the on-disk order.
type Column struct {
	Name string
	Type ColumnType
}

type Reference struct{ Table, Column string }
type ColumnConstraint struct {
	NotNull, Unique bool
	References      *Reference
}

// Schema describes the columns a Table accepts.
type Schema struct {
	Columns     []Column
	Constraints map[string]ColumnConstraint
}

// Row is a named set of column values.
type Row map[string]any

// Table gives typed row semantics to a Store. The key is the table's primary key.
type Table struct {
	store  *Store
	schema Schema
	// secondary maps a typed column value to primary keys. It is rebuilt from
	// the durable primary-key index when a table is opened.
	secondary map[string]map[string]map[string]struct{}
}

func NewTable(store *Store, schema Schema) (*Table, error) {
	if store == nil {
		return nil, fmt.Errorf("nil store")
	}
	if err := schema.validate(); err != nil {
		return nil, err
	}
	t := &Table{store: store, schema: schema, secondary: map[string]map[string]map[string]struct{}{}}
	if len(schema.Columns) == 0 {
		return nil, fmt.Errorf("schema has no columns")
	}
	for _, c := range schema.Columns[1:] {
		t.secondary[c.Name] = map[string]map[string]struct{}{}
	}
	rows, err := t.Scan()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		t.indexRow(fmt.Sprint(row[schema.Columns[0].Name]), row)
	}
	return t, nil
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
	if err := t.validateConstraints(key, row); err != nil {
		return err
	}
	old, found, err := t.Get(key)
	if err != nil {
		return err
	}
	if err = t.store.Put([]byte(key), b); err != nil {
		return err
	}
	if found {
		t.unindexRow(key, old)
	}
	t.indexRow(key, row)
	return nil
}

func (t *Table) validateConstraints(key string, row Row) error {
	rows, err := t.Scan()
	if err != nil {
		return err
	}
	for _, c := range t.schema.Columns {
		if !t.schema.Constraints[c.Name].Unique || row[c.Name] == nil {
			continue
		}
		for _, old := range rows {
			if fmt.Sprint(old[t.schema.Columns[0].Name]) != key && old[c.Name] == row[c.Name] {
				return fmt.Errorf("unique constraint failed: %s", c.Name)
			}
		}
	}
	return nil
}

func (t *Table) Get(key string) (Row, bool, error) {
	b, found, err := t.store.Get([]byte(key))
	if err != nil || !found {
		return nil, found, err
	}
	r, err := t.decode(b)
	return r, true, err
}

// Snapshot returns a stable read point for this table's store.
func (t *Table) Snapshot() Snapshot { return t.store.BeginSnapshot() }

// GetAt reads a row as it existed at snapshot.
func (t *Table) GetAt(snapshot Snapshot, key string) (Row, bool, error) {
	b, found, err := t.store.GetAt(snapshot, []byte(key))
	if err != nil || !found {
		return nil, found, err
	}
	r, err := t.decode(b)
	return r, true, err
}

// ScanAt returns all rows visible at snapshot.
func (t *Table) ScanAt(snapshot Snapshot) ([]Row, error) {
	rows := make([]Row, 0)
	for _, key := range t.store.KeysAt(snapshot) {
		row, found, err := t.GetAt(snapshot, string(key))
		if err != nil {
			return nil, err
		}
		if found {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (t *Table) Delete(key string) error {
	old, found, err := t.Get(key)
	if err != nil || !found {
		return err
	}
	if err = t.store.Delete([]byte(key)); err == nil {
		t.unindexRow(key, old)
	}
	return err
}

func indexValue(v any) string { return fmt.Sprintf("%T:%#v", v, v) }

func (t *Table) indexRow(key string, row Row) {
	for _, c := range t.schema.Columns[1:] {
		v := indexValue(row[c.Name])
		if t.secondary[c.Name][v] == nil {
			t.secondary[c.Name][v] = map[string]struct{}{}
		}
		t.secondary[c.Name][v][key] = struct{}{}
	}
}
func (t *Table) unindexRow(key string, row Row) {
	for _, c := range t.schema.Columns[1:] {
		v := indexValue(row[c.Name])
		delete(t.secondary[c.Name][v], key)
		if len(t.secondary[c.Name][v]) == 0 {
			delete(t.secondary[c.Name], v)
		}
	}
}

// Find returns rows matching an equality predicate on a secondary-indexed column.
func (t *Table) Find(column string, value any) ([]Row, error) {
	keys, ok := t.secondary[column][indexValue(value)]
	if !ok {
		return nil, nil
	}
	rows := make([]Row, 0, len(keys))
	for key := range keys {
		row, found, err := t.Get(key)
		if err != nil {
			return nil, err
		}
		if found {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// Scan returns every currently live row. It is intentionally a full scan;
// callers that need efficient point lookups should use Get.
func (t *Table) Scan() ([]Row, error) {
	var rows []Row
	if t.store.index.root == nil {
		return rows, nil
	}
	leaf := t.store.index.root
	for !leaf.leaf {
		leaf = leaf.children[0]
	}
	for ; leaf != nil; leaf = leaf.next {
		for _, value := range leaf.values {
			row, err := t.decode(value)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// Update changes matching rows and returns the number changed.
func (t *Table) Update(where func(Row) bool, set Row) (int, error) {
	rows, err := t.Scan()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if where != nil && !where(r) {
			continue
		}
		for k, v := range set {
			r[k] = v
		}
		key := fmt.Sprint(r[t.schema.Columns[0].Name])
		if err = t.Insert(key, r); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (t *Table) DeleteWhere(where func(Row) bool) (int, error) {
	rows, err := t.Scan()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if where != nil && !where(r) {
			continue
		}
		if err = t.Delete(fmt.Sprint(r[t.schema.Columns[0].Name])); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Alter changes the schema and rewrites live rows using zero values for new columns.
func (t *Table) Alter(schema Schema) error {
	if err := schema.validate(); err != nil {
		return err
	}
	old := t.schema
	rows, err := t.Scan()
	if err != nil {
		return err
	}
	for _, r := range rows {
		nr := Row{}
		for _, c := range schema.Columns {
			if v, ok := r[c.Name]; ok {
				nr[c.Name] = v
			} else {
				switch c.Type {
				case IntType:
					nr[c.Name] = 0
				case StringType:
					nr[c.Name] = ""
				case BoolType:
					nr[c.Name] = false
				case FloatType:
					nr[c.Name] = float64(0)
				case BytesType:
					nr[c.Name] = []byte{}
				case TimestampType:
					nr[c.Name] = time.Time{}
				}
			}
		}
		if err = t.Insert(fmt.Sprint(r[old.Columns[0].Name]), nr); err != nil {
			return err
		}
	}
	t.schema = schema
	return nil
}

func (s Schema) validate() error {
	seen := map[string]bool{}
	for _, c := range s.Columns {
		if c.Name == "" || seen[c.Name] {
			return fmt.Errorf("invalid or duplicate column %q", c.Name)
		}
		if c.Type < IntType || c.Type > TimestampType {
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
	nullBytes := (len(t.schema.Columns) + 7) / 8
	b.Write(make([]byte, nullBytes))
	for _, c := range t.schema.Columns {
		v, ok := row[c.Name]
		if !ok {
			return nil, fmt.Errorf("missing column %q", c.Name)
		}
		if v == nil {
			if t.schema.Constraints[c.Name].NotNull {
				return nil, fmt.Errorf("column %q may not be NULL", c.Name)
			}
			b.Bytes()[indexOf(t.schema.Columns, c)/8] |= 1 << uint(indexOf(t.schema.Columns, c)%8)
			continue
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
			bv, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("column %q expects bool", c.Name)
			}
			if bv {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
		case FloatType:
			f, ok := v.(float64)
			if !ok {
				if i, ok2 := v.(int); ok2 {
					f = float64(i)
				} else {
					return nil, fmt.Errorf("column %q expects float", c.Name)
				}
			}
			if err := binary.Write(&b, binary.LittleEndian, f); err != nil {
				return nil, err
			}
		case BytesType:
			v, ok := v.([]byte)
			if !ok {
				return nil, fmt.Errorf("column %q expects bytes", c.Name)
			}
			if err := binary.Write(&b, binary.LittleEndian, uint32(len(v))); err != nil {
				return nil, err
			}
			b.Write(v)
		case TimestampType:
			v, ok := v.(time.Time)
			if !ok {
				return nil, fmt.Errorf("column %q expects timestamp", c.Name)
			}
			if err := binary.Write(&b, binary.LittleEndian, v.UnixNano()); err != nil {
				return nil, err
			}
		}
	}
	return b.Bytes(), nil
}

func (t *Table) decode(data []byte) (Row, error) {
	r := Row{}
	nullBytes := (len(t.schema.Columns) + 7) / 8
	if len(data) < nullBytes {
		return nil, fmt.Errorf("row missing NULL bitmap")
	}
	nulls := data[:nullBytes]
	p := nullBytes
	for _, c := range t.schema.Columns {
		i := indexOf(t.schema.Columns, c)
		if nulls[i/8]&(1<<uint(i%8)) != 0 {
			r[c.Name] = nil
			continue
		}
		switch c.Type {
		case IntType:
			if p+8 > len(data) {
				return nil, fmt.Errorf("column %q (int): need 8 bytes at offset %d, row has %d bytes", c.Name, p, len(data))
			}
			r[c.Name] = int(int64(binary.LittleEndian.Uint64(data[p : p+8])))
			p += 8
		case StringType:
			if p+4 > len(data) {
				return nil, fmt.Errorf("column %q (string): missing 4-byte length at offset %d, row has %d bytes", c.Name, p, len(data))
			}
			n := int(binary.LittleEndian.Uint32(data[p : p+4]))
			p += 4
			if n < 0 || p+n > len(data) {
				return nil, fmt.Errorf("column %q (string): length %d exceeds row data at offset %d (row has %d bytes)", c.Name, n, p, len(data))
			}
			r[c.Name] = string(data[p : p+n])
			p += n
		case BoolType:
			if p >= len(data) || data[p] > 1 {
				return nil, fmt.Errorf("column %q (bool): expected one byte with value 0 or 1 at offset %d (row has %d bytes)", c.Name, p, len(data))
			}
			r[c.Name] = data[p] == 1
			p++
		case FloatType:
			if p+8 > len(data) {
				return nil, fmt.Errorf("column %q (float): need 8 bytes", c.Name)
			}
			r[c.Name] = binary.LittleEndian.Uint64(data[p : p+8])
			p += 8
			// Convert the IEEE bits without importing math in the common path.
			r[c.Name] = math.Float64frombits(r[c.Name].(uint64))
		case BytesType:
			if p+4 > len(data) {
				return nil, fmt.Errorf("column %q (bytes): missing length", c.Name)
			}
			n := int(binary.LittleEndian.Uint32(data[p : p+4]))
			p += 4
			if p+n > len(data) {
				return nil, fmt.Errorf("column %q (bytes): invalid length", c.Name)
			}
			r[c.Name] = append([]byte(nil), data[p:p+n]...)
			p += n
		case TimestampType:
			if p+8 > len(data) {
				return nil, fmt.Errorf("column %q (timestamp): need 8 bytes", c.Name)
			}
			ns := int64(binary.LittleEndian.Uint64(data[p : p+8]))
			p += 8
			r[c.Name] = time.Unix(0, ns).UTC()
		}
	}
	if p != len(data) {
		return nil, fmt.Errorf("trailing row data: schema consumed %d of %d bytes", p, len(data))
	}
	return r, nil
}

func indexOf(cs []Column, want Column) int {
	for i, c := range cs {
		if c.Name == want.Name {
			return i
		}
	}
	return 0
}
