package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type dbTest struct {
	db  KV
	ref map[string]string
}

func newDBTest(t *testing.T) *dbTest {
	t.Helper()
	d := &dbTest{ref: map[string]string{}}
	d.db.Path = filepath.Join(t.TempDir(), "test.db")
	if err := d.db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(d.db.Close)
	return d
}

// test close and reopen file
func (d *dbTest) reopen(t *testing.T) {
	t.Helper()
	d.db.Close()
	if err := d.db.Open(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

func (d *dbTest) set(t *testing.T, key string, val string) {
	t.Helper()
	if err := d.db.Set([]byte(key), []byte(val)); err != nil {
		t.Fatalf("set %q: %v", key, err)
	}
	d.ref[key] = val
}

func (d *dbTest) del(t *testing.T, key string) bool {
	t.Helper()
	deleted, err := d.db.Del([]byte(key))
	if err != nil {
		t.Fatalf("del %q: %v", key, err)
	}
	delete(d.ref, key)
	return deleted
}

func (d *dbTest) verify(t *testing.T) {
	t.Helper()
	for k, want := range d.ref {
		got, ok := d.db.Get([]byte(k))
		if !ok {
			t.Fatalf("missing key %q", k)
		}
		if string(got) != want {
			t.Fatalf("key %q = %q, want %q", k, got, want)
		}
	}
	if _, ok := d.db.Get([]byte("this-key-does-not-exist")); ok {
		t.Fatal("found a key that was never inserted")
	}
}

// tests

func TestKVBasic(t *testing.T) {
	d := newDBTest(t)
	d.set(t, "k", "v")
	d.verify(t)
	d.reopen(t)
	d.verify(t)

	for i := 0; i < 5000; i++ {
		d.set(t, fmt.Sprintf("key%d", i), fmt.Sprintf("vvv%d", i))
	}
	d.verify(t)
	d.reopen(t)
	d.verify(t)

	// overwrite existing keys
	for i := 0; i < 5000; i += 3 {
		d.set(t, fmt.Sprintf("key%d", i), fmt.Sprintf("new%d", i))
	}
	d.reopen(t)
	d.verify(t)

	for i := 0; i < 5000; i++ {
		if !d.del(t, fmt.Sprintf("key%d", i)) {
			t.Fatalf("del key%d returned false", i)
		}
	}
	d.reopen(t)
	d.verify(t)

	if deleted, _ := d.db.Del([]byte("nope")); deleted {
		t.Fatal("deleted a key that was never there")
	}
}

func TestKVFileGrows(t *testing.T) {
	d := newDBTest(t)
	for i := 0; i < 2000; i++ {
		val := make([]byte, BTREE_MAX_VAL_SIZE)
		for j := range val {
			val[j] = byte('a' + i%26)
		}
		d.set(t, fmt.Sprintf("key%d", i), string(val))
	}
	d.reopen(t)
	d.verify(t)

	st, err := os.Stat(d.db.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size()%BTREE_PAGE_SIZE != 0 {
		t.Fatalf("file size %d is not page aligned", st.Size())
	}
	t.Logf("file size: %d bytes, %d pages", st.Size(), d.db.page.flushed)
}

// grow the file past the initial 64MB mapping to exercise the 2nd mmap chunk.
// with page reuse the file only grows when the tree does, so the tree itself
// must exceed 64MB: each max-size value fills its own leaf (~4KB per key).
func TestKVManyChunks(t *testing.T) {
	d := newDBTest(t)
	val := string(make([]byte, BTREE_MAX_VAL_SIZE))
	for i := 0; i < 20000; i++ {
		d.set(t, fmt.Sprintf("key%d", i), val)
	}
	if len(d.db.mmap.chunks) < 2 {
		t.Fatalf("expected more than 1 mmap chunk, got %d", len(d.db.mmap.chunks))
	}
	d.reopen(t)
	d.verify(t)
}

func TestKVBadSignature(t *testing.T) {
	d := newDBTest(t)
	d.set(t, "a", "1")
	d.db.Close()

	f, err := os.OpenFile(d.db.Path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("NotADatabase!!!!"), 0); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := d.db.Open(); err == nil {
		t.Fatal("opened a file with a bad signature")
	}
}

// the whole point of chapter 07: repeated updates must reuse pages
func TestKVReuse(t *testing.T) {
	d := newDBTest(t)
	val := string(make([]byte, BTREE_MAX_VAL_SIZE))
	for i := 0; i < 500; i++ {
		d.set(t, fmt.Sprintf("key%d", i), val)
	}
	d.verify(t)
	grown := d.db.page.flushed

	// overwrite every key many times over; the tree size never changes
	for round := 0; round < 20; round++ {
		for i := 0; i < 500; i++ {
			d.set(t, fmt.Sprintf("key%d", i), val)
		}
	}
	d.reopen(t)
	d.verify(t)

	// allow some slack for free list nodes and transient growth
	if d.db.page.flushed > grown+grown/4 {
		t.Fatalf("file grew from %d to %d pages; pages are not being reused",
			grown, d.db.page.flushed)
	}
	t.Logf("%d pages after 10000 overwrites of a %d-page tree", d.db.page.flushed, grown)
}

// insert and delete in a loop: the high-water mark must be bounded
func TestKVChurn(t *testing.T) {
	d := newDBTest(t)
	val := string(make([]byte, 500))
	var highWater uint64
	for round := 0; round < 30; round++ {
		for i := 0; i < 200; i++ {
			d.set(t, fmt.Sprintf("key%d", i), val)
		}
		for i := 0; i < 200; i++ {
			if !d.del(t, fmt.Sprintf("key%d", i)) {
				t.Fatalf("round %d: del key%d returned false", round, i)
			}
		}
		d.verify(t)
		if round == 0 {
			// the first cycle sets the baseline; later cycles should reuse its pages
			highWater = d.db.page.flushed
			continue
		}
		if d.db.page.flushed > highWater+highWater/4 {
			t.Fatalf("round %d: file grew from %d to %d pages; churn is not bounded",
				round, highWater, d.db.page.flushed)
		}
	}
	d.reopen(t)
	d.verify(t)
	t.Logf("file is %d pages after 30 fill/empty cycles", d.db.page.flushed)
}

// the free list survives close/reopen with its position intact
func TestKVFreeListPersists(t *testing.T) {
	d := newDBTest(t)
	for i := 0; i < 1000; i++ {
		d.set(t, fmt.Sprintf("key%d", i), "v")
	}
	for i := 0; i < 500; i++ {
		d.del(t, fmt.Sprintf("key%d", i))
	}
	head, hseq := d.db.free.headPage, d.db.free.headSeq
	tail, tseq := d.db.free.tailPage, d.db.free.tailSeq

	d.reopen(t)
	if d.db.free.headPage != head || d.db.free.headSeq != hseq ||
		d.db.free.tailPage != tail || d.db.free.tailSeq != tseq {
		t.Fatalf("free list changed across reopen: (%d,%d,%d,%d) -> (%d,%d,%d,%d)",
			head, hseq, tail, tseq,
			d.db.free.headPage, d.db.free.headSeq,
			d.db.free.tailPage, d.db.free.tailSeq)
	}
	if d.db.free.maxSeq != d.db.free.tailSeq {
		t.Fatalf("maxSeq %d != tailSeq %d after reopen; loadMeta did not restore it",
			d.db.free.maxSeq, d.db.free.tailSeq)
	}
	// and it still works
	for i := 0; i < 500; i++ {
		d.set(t, fmt.Sprintf("new%d", i), "v")
	}
	d.reopen(t)
	d.verify(t)
}

// an empty DB must have its initial free list node on disk, not in a hole
func TestKVInitialFreeListPage(t *testing.T) {
	d := newDBTest(t)
	d.set(t, "k", "v")
	st, err := os.Stat(d.db.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() < 3*BTREE_PAGE_SIZE {
		t.Fatalf("file is %d bytes; want at least meta + free list node + root",
			st.Size())
	}
	d.reopen(t)
	d.verify(t)
}
