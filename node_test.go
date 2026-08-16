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
	
	tree := newMemoryTree() 
	tree.Insert([]byte("key"), []byte("hello"))

}