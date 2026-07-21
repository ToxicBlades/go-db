package kv

import "bytes"

// btree is a B+Tree. Leaves contain the values; internal keys are
// separators (the first key in the child to their right). It is rebuilt from
// the append-only record log when a Store is opened if its persisted index is
// unavailable.
const btreeOrder = 32

type btreeNode struct {
	leaf     bool
	keys     [][]byte
	values   [][]byte
	children []*btreeNode
	next     *btreeNode
}

type btree struct{ root *btreeNode }

type btreeEntry struct{ Key, Value []byte }

func (t *btree) entries() []btreeEntry {
	var out []btreeEntry
	if t.root == nil {
		return out
	}
	n := t.root
	for !n.leaf {
		n = n.children[0]
	}
	for ; n != nil; n = n.next {
		for i := range n.keys {
			out = append(out, btreeEntry{copyKey(n.keys[i]), append([]byte(nil), n.values[i]...)})
		}
	}
	return out
}

func (t *btree) get(key []byte) ([]byte, bool) {
	if t.root == nil {
		return nil, false
	}
	n := t.root
	for !n.leaf {
		n = n.children[childIndex(n.keys, key)]
	}
	i := keyIndex(n.keys, key)
	if i < len(n.keys) && bytes.Equal(n.keys[i], key) {
		return n.values[i], true
	}
	return nil, false
}

func childIndex(keys [][]byte, key []byte) int {
	i := 0
	for i < len(keys) && bytes.Compare(key, keys[i]) >= 0 {
		i++
	}
	return i
}

func keyIndex(keys [][]byte, key []byte) int {
	lo, hi := 0, len(keys)
	for lo < hi {
		m := (lo + hi) / 2
		if bytes.Compare(keys[m], key) < 0 {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}
func copyKey(k []byte) []byte { return append([]byte(nil), k...) }

func (t *btree) put(key, value []byte) {
	if t.root == nil {
		t.root = &btreeNode{leaf: true, keys: [][]byte{copyKey(key)}, values: [][]byte{append([]byte(nil), value...)}}
		return
	}
	if len(t.root.keys) == btreeOrder {
		old := t.root
		t.root = &btreeNode{children: []*btreeNode{old}}
		t.root.leaf = false
		t.splitChild(t.root, 0)
	}
	t.insertNonFull(t.root, key, value)
}

func (t *btree) splitChild(parent *btreeNode, i int) {
	old := parent.children[i]
	mid := len(old.keys) / 2
	var right *btreeNode
	var sep []byte
	if old.leaf {
		right = &btreeNode{leaf: true, keys: append([][]byte(nil), old.keys[mid:]...), values: append([][]byte(nil), old.values[mid:]...)}
		sep = copyKey(right.keys[0])
		right.next = old.next
		old.next = right
		old.keys = old.keys[:mid]
		old.values = old.values[:mid]
	} else {
		cut := len(old.children) / 2
		sep = copyKey(old.keys[cut-1])
		right = &btreeNode{children: append([]*btreeNode(nil), old.children[cut:]...), keys: append([][]byte(nil), old.keys[cut:]...)}
		old.children = old.children[:cut]
		old.keys = old.keys[:cut-1]
	}
	parent.keys = append(parent.keys, nil)
	copy(parent.keys[i+1:], parent.keys[i:])
	parent.keys[i] = sep
	parent.children = append(parent.children, nil)
	copy(parent.children[i+2:], parent.children[i+1:])
	parent.children[i+1] = right
}

func (t *btree) insertNonFull(n *btreeNode, key, value []byte) {
	if n.leaf {
		i := keyIndex(n.keys, key)
		if i < len(n.keys) && bytes.Equal(n.keys[i], key) {
			n.values[i] = append([]byte(nil), value...)
			return
		}
		n.keys = append(n.keys, nil)
		copy(n.keys[i+1:], n.keys[i:])
		n.keys[i] = copyKey(key)
		n.values = append(n.values, nil)
		copy(n.values[i+1:], n.values[i:])
		n.values[i] = append([]byte(nil), value...)
		return
	}
	i := childIndex(n.keys, key)
	if len(n.children[i].keys) == btreeOrder {
		t.splitChild(n, i)
		if bytes.Compare(key, n.keys[i]) >= 0 {
			i++
		}
	}
	t.insertNonFull(n.children[i], key, value)
}

func (t *btree) delete(key []byte) bool {
	if _, ok := t.get(key); !ok {
		return false
	}
	// Repack from the leaf chain. This keeps deletion simple and guarantees
	// that every underfull path is merged; insertion then splits as needed.
	var leaf *btreeNode
	if t.root != nil {
		leaf = t.root
		for !leaf.leaf {
			leaf = leaf.children[0]
		}
	}
	var keys, values [][]byte
	for leaf != nil {
		for i, k := range leaf.keys {
			if !bytes.Equal(k, key) {
				keys = append(keys, k)
				values = append(values, leaf.values[i])
			}
		}
		leaf = leaf.next
	}
	t.root = nil
	for i, k := range keys {
		t.put(k, values[i])
	}
	return true
}
