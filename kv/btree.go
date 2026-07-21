package kv

import "bytes"

// btree is an in-memory B+Tree. Leaves contain the values; internal keys are
// separators (the first key in the child to their right). It is rebuilt from
// the append-only record log when a Store is opened.
const btreeOrder = 32

type btreeNode struct {
	leaf     bool
	keys     [][]byte
	values   [][]byte
	children []*btreeNode
	next     *btreeNode
}

type btree struct{ root *btreeNode }

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
func (t *btree) deleteNode(n *btreeNode, key []byte) bool {
	if n.leaf {
		i := keyIndex(n.keys, key)
		if i == len(n.keys) || !bytes.Equal(n.keys[i], key) {
			return false
		}
		copy(n.keys[i:], n.keys[i+1:])
		n.keys = n.keys[:len(n.keys)-1]
		copy(n.values[i:], n.values[i+1:])
		n.values = n.values[:len(n.values)-1]
		return true
	}
	if len(n.children) == 0 {
		return false
	}
	i := childIndex(n.keys, key)
	if i >= len(n.children) {
		i = len(n.children) - 1
	}
	if !t.deleteNode(n.children[i], key) {
		return false
	}
	min := (btreeOrder + 1) / 2
	if len(n.children[i].keys) < min {
		t.rebalance(n, i)
	}
	// Refresh separators after a deletion or merge.
	for j := range n.keys {
		if len(n.children[j+1].keys) > 0 {
			n.keys[j] = copyKey(firstKey(n.children[j+1]))
		}
	}
	return true
}
func firstKey(n *btreeNode) []byte {
	for !n.leaf {
		i := 0
		for i < len(n.children) && len(n.children[i].keys) == 0 {
			i++
		}
		if i == len(n.children) {
			return nil
		}
		n = n.children[i]
	}
	if len(n.keys) == 0 {
		return nil
	}
	return n.keys[0]
}
func (t *btree) rebalance(p *btreeNode, i int) {
	min := (btreeOrder + 1) / 2
	c := p.children[i]
	if i > 0 && len(p.children[i-1].keys) > min {
		l := p.children[i-1]
		if c.leaf {
			c.keys = append([][]byte{l.keys[len(l.keys)-1]}, c.keys...)
			c.values = append([][]byte{l.values[len(l.values)-1]}, c.values...)
			l.keys = l.keys[:len(l.keys)-1]
			l.values = l.values[:len(l.values)-1]
		} else {
			c.children = append([]*btreeNode{l.children[len(l.children)-1]}, c.children...)
			l.children = l.children[:len(l.children)-1]
		}
		return
	}
	if i+1 < len(p.children) && len(p.children[i+1].keys) > min {
		r := p.children[i+1]
		if c.leaf {
			c.keys = append(c.keys, r.keys[0])
			c.values = append(c.values, r.values[0])
			r.keys = r.keys[1:]
			r.values = r.values[1:]
		} else {
			c.children = append(c.children, r.children[0])
			r.children = r.children[1:]
		}
		return
	}
	if i > 0 {
		t.merge(p, i-1)
	} else {
		t.merge(p, i)
	}
}
func (t *btree) merge(p *btreeNode, i int) {
	l, r := p.children[i], p.children[i+1]
	if l.leaf {
		l.keys = append(l.keys, r.keys...)
		l.values = append(l.values, r.values...)
		l.next = r.next
	} else {
		l.children = append(l.children, r.children...)
	}
	copy(p.children[i+1:], p.children[i+2:])
	p.children = p.children[:len(p.children)-1]
	copy(p.keys[i:], p.keys[i+1:])
	p.keys = p.keys[:len(p.keys)-1]
}
