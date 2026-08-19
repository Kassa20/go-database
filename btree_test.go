package main

import (
	"fmt"
	"sort"
	"testing"
	"unsafe"
)

type treeTest struct {
	tree  BTree
	ref   map[string]string // reference to check against
	pages map[uint64]BNode  // in-memory store
}

func newTreeTest() *treeTest {
	pages := map[uint64]BNode{}
	return &treeTest{
		tree: BTree{
			get: func(ptr uint64) []byte {
				node, ok := pages[ptr]
				assert(ok)
				return node
			},
			new: func(node []byte) uint64 {
				assert(BNode(node).nbytes() <= BTREE_PAGE_SIZE)
				ptr := uint64(uintptr(unsafe.Pointer(&node[0])))
				assert(pages[ptr] == nil)
				pages[ptr] = node
				return ptr
			},
			del: func(ptr uint64) {
				assert(pages[ptr] != nil)
				delete(pages, ptr)
			},
		},
		ref: map[string]string{},
		pages: pages,
	}
}

func (treeTest *treeTest) insert(key string, val string) {
	if err := treeTest.tree.Insert([]byte(key), []byte(val)); err != nil {
		panic(err)
	}
	treeTest.ref[key] = val
}

func (treeTest *treeTest) remove(key string) bool {
	deleted, err := treeTest.tree.Delete([]byte(key))
	if err != nil {
		panic(err)
	}
	delete(treeTest.ref, key)
	return deleted
}

func (treeTest *treeTest) dump() ([]string, []string) {
	keys, vals := []string{}, []string{}
	var walk func(uint64)
	walk = func(ptr uint64) {
		node := BNode(treeTest.tree.get(ptr))
		nkeys := node.nkeys()

		if node.btype() == BNODE_LEAF {
			for i := uint16(0); i < nkeys; i++ {
				keys = append(keys, string(node.getKey(i)))
				vals = append(vals, string(node.getVal(i)))
			}
		} else {
			for i := uint16(0); i < nkeys; i++ {
				walk(node.getPtr(i))
			}
		}
	}
	if treeTest.tree.root == 0 {
		return keys, vals
	}
	walk(treeTest.tree.root)
	assert(keys[0] == "")

	return keys[1:], vals[1:]
}

func (treeTest *treeTest) verifyTree(t *testing.T) {
	t.Helper()

	if treeTest.tree.root != 0 {
		var walk func(ptr uint64)
		walk = func(ptr uint64) {
			node := BNode(treeTest.tree.get(ptr))
			if node.nbytes() > BTREE_PAGE_SIZE {
				t.Fatalf("oversized node: %d bytes", node.nbytes())
			}
			for i := uint16(1); i < node.nkeys(); i++ {
				if string(node.getKey(i-1)) >= string(node.getKey(i)) {
					t.Fatalf("keys out of order in node %d", ptr)
				}
			}
			if node.btype() == BNODE_NODE {
				for i := uint16(0); i < node.nkeys(); i++ {
					kid := BNode(treeTest.tree.get(node.getPtr(i)))
					if string(kid.getKey(0)) != string(node.getKey(i)) {
						t.Fatalf("kid %d key mismatch", i)
					}
					walk(node.getPtr(i))
				}
			}
		}
		walk(treeTest.tree.root)
	}

	keys, vals := treeTest.dump()
	if len(keys) != len(treeTest.ref) {
		t.Fatalf("tree has %d keys, ref has %d", len(keys), len(treeTest.ref))
	}
	want := make([]string, 0, len(treeTest.ref))
	for k := range treeTest.ref {
		want = append(want, k)
	}
	sort.Strings(want)
	for i, k := range keys {
		if k != want[i] {
			t.Fatalf("key %d = %q, want %q", i, k, want[i])
		}	
		if vals[i] != treeTest.ref[k] {
			t.Fatalf("val for %q = %q, want %q", k, vals[i], treeTest.ref[k])
		}	
	}
}

func TestBasic(t *testing.T) {
	tree := newTreeTest()
	tree.insert("k", "v")
	tree.verifyTree(t)

	// insert a lot to force splits
	for i := 0; i < 25000; i++ {
		tree.insert(fmt.Sprintf("key%d", i), fmt.Sprintf("vvv%d", i))
	}
	tree.verifyTree(t)

	// delete a lot to force merges
	for i := 0; i < 25000; i++ {
		tree.remove(fmt.Sprintf("key%d", i))
	}
	tree.verifyTree(t)
}

func TestBigValues(t *testing.T) {
	tree := newTreeTest()
	for i := 0; i < 400; i++ {
		val := make([]byte, BTREE_MAX_VAL_SIZE)
		for j := range val {
			val[j] = byte('a' + i%26)
		}
		tree.insert(fmt.Sprintf("key%d", i), string(val))
	}
	tree.verifyTree(t)
	for i := 0; i < 400; i++ {
		if !tree.remove(fmt.Sprintf("key%d", i)) {
			t.Fatalf("delete key%d returned false", i)
		}	
	}
	tree.verifyTree(t)
}

func TestDeleteMissing(t *testing.T) {
	tree := newTreeTest()
	tree.insert("a", "1")
	if deleted, _ := tree.tree.Delete([]byte("fake")); deleted {
		t.Fatal("deleted a key that was never there")
	}
	tree.verifyTree(t)
}