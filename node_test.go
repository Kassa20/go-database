package main

import (
	// "testing"
	"testing"
	"unsafe"
)

func newMemoryTree() *BTree {
	pages := map[uint64][]byte{}
	return &BTree{
		get: func(ptr uint64) []byte {
			page, ok := pages[ptr]
			if !ok {
				panic("bad page read")
			}
			return page
		},
		new: func(node []byte) uint64 {
			if len(node) > BTREE_PAGE_SIZE {
				panic("oversized page")
			}

			ptr := uint64(uintptr(unsafe.Pointer(&node[0])))
			pages[ptr] = node
			return ptr
		},
		del: func(ptr uint64) {
			delete(pages, ptr)
		},
	}
}

func TestLeafInsert(t *testing.T) {
	old := BNode(make([]byte, BTREE_PAGE_SIZE))
	old.setHeader(BNODE_LEAF, 2)

	//keys
	nodeAppendKV(old, 0, 0, []byte("key1"), []byte("hi"))
	nodeAppendKV(old, 1, 0, []byte("key3"), []byte("hello"))

	new := BNode(make([]byte, BTREE_PAGE_SIZE))
	leafInsert(new, old, 1, []byte("key2"), []byte("there"))

	if new.nkeys() != 3 {
		t.Fatalf("nkeys = %d, want 3", new.nkeys())
	}

	want := []string{"key1", "key2", "key3"}
	for i, w := range want {
		if got := string(new.getKey(uint16(i))); got != w {
			t.Errorf("key %d = %q, want %q", i, got, w)
		}
	}
	if got := string(new.getVal(1)); got != "there" {
		t.Errorf("val 1 = %q, want %q", got, "there")
	}

}