package main

import "testing"

type flTest struct {
	fl    FreeList
	pages map[uint64][]byte
	next  uint64
}

func newFLTest() *flTest {
	f := &flTest{pages: map[uint64][]byte{}, next: 1}
	// a pushed pointer names a real page in the file, so back any pointer
	f.fl.get = func(ptr uint64) []byte { return f.page(ptr) }
	f.fl.set = func(ptr uint64) []byte { return f.page(ptr) }
	f.fl.new = func(node []byte) uint64 {
		ptr := f.next
		f.next++
		f.pages[ptr] = node
		return ptr
	}
	// seed the initial node, like readRoot does on an empty DB
	f.fl.headPage = f.fl.new(make([]byte, BTREE_PAGE_SIZE))
	f.fl.tailPage = f.fl.headPage
	return f
}

// page returns the backing bytes for ptr, materializing it on first use.
// Only `new` counts as allocating a list node.
func (f *flTest) page(ptr uint64) []byte {
	node, ok := f.pages[ptr]
	if !ok {
		node = make([]byte, BTREE_PAGE_SIZE)
		f.pages[ptr] = node
	}
	return node
}

func TestFreeListEmpty(t *testing.T) {
	f := newFLTest()
	if ptr := f.fl.PopHead(); ptr != 0 {
		t.Fatalf("popped %d from an empty list", ptr)
	}
}

// items pushed in update N are only poppable after SetMaxSeq
func TestFreeListMaxSeq(t *testing.T) {
	f := newFLTest()
	f.fl.PushTail(100)
	if ptr := f.fl.PopHead(); ptr != 0 {
		t.Fatalf("popped %d before SetMaxSeq", ptr)
	}
	f.fl.SetMaxSeq()
	if ptr := f.fl.PopHead(); ptr != 100 {
		t.Fatalf("popped %d, want 100", ptr)
	}
}

// FIFO order across many list nodes
func TestFreeListManyNodes(t *testing.T) {
	f := newFLTest()
	const n = FREE_LIST_CAP * 5
	for i := 1; i <= n; i++ {
		f.fl.PushTail(uint64(1000 + i))
	}
	f.fl.SetMaxSeq()

	got := map[uint64]bool{}
	for i := 0; i < n; i++ {
		ptr := f.fl.PopHead()
		if ptr == 0 {
			t.Fatalf("list ran dry after %d pops", i)
		}
		if got[ptr] {
			t.Fatalf("page %d handed out twice", ptr)
		}
		got[ptr] = true
	}
	// everything pushed comes back, plus the recycled list nodes
	for i := 1; i <= n; i++ {
		if !got[uint64(1000+i)] {
			t.Fatalf("never got back page %d", 1000+i)
		}
	}
	if f.fl.headPage == 0 || f.fl.tailPage == 0 {
		t.Fatal("the list lost its last node")
	}
}

// the list must not consume unbounded new pages: it reuses its own
func TestFreeListSelfHosting(t *testing.T) {
	f := newFLTest()
	for round := 0; round < 20; round++ {
		for i := 0; i < FREE_LIST_CAP; i++ {
			f.fl.PushTail(uint64(100000 + round*FREE_LIST_CAP + i))
		}
		f.fl.SetMaxSeq()
		for i := 0; i < FREE_LIST_CAP; i++ {
			if f.fl.PopHead() == 0 {
				t.Fatal("unexpected empty list")
			}
		}
	}
	// `new` should have been called only a handful of times
	if f.next > 8 {
		t.Fatalf("allocated %d list nodes; the list is not reusing its own", f.next-1)
	}
}
