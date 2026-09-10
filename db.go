package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"syscall"
)

const DB_SIG = "BuildYourOwnDB06"

type KV struct {
	Path string
	fd   int
	tree BTree
	free FreeList

	mmap struct {
		total  int
		chunks [][]byte
	}
	page struct {
		flushed uint64            // database size in number of pages
		nappend uint64            // number of pages to be appended
		updates map[uint64][]byte // pending updates + appended pages
	}
	failed bool // did the last update fail?
}

// open or create the file, and fsync the parent directory
func createFileSync(file string) (int, error) {
	flags := os.O_RDONLY | syscall.O_DIRECTORY
	dirfd, err := syscall.Open(path.Dir(file), flags, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open directory: %w", err)
	}
	defer syscall.Close(dirfd)

	// open or create the file
	fd, err := syscall.Open(file, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open file: %w", err)
	}

	// fsync the directory
	if err = syscall.Fsync(dirfd); err != nil {
		_ = syscall.Close(fd) // may leave an empty file
		return -1, fmt.Errorf("fsync directory: %w", err)
	}
	return fd, nil
}

// read a page from disk
func (db *KV) pageReadFile(ptr uint64) []byte {
	start := uint64(0)
	for _, chunk := range db.mmap.chunks {
		end := start + uint64(len(chunk))/BTREE_PAGE_SIZE
		if ptr < end {
			offset := BTREE_PAGE_SIZE * (ptr - start)
			return chunk[offset : offset+BTREE_PAGE_SIZE]
		}
		start = end
	}
	panic("bad ptr")
}

func (db *KV) pageRead(ptr uint64) []byte {
	if node, ok := db.page.updates[ptr]; ok {
		return node
	}
	return db.pageReadFile(ptr)
}

// add a new page, written to disk later. return its page number
func (db *KV) pageAppend(node []byte) uint64 {
	assert(len(node) == BTREE_PAGE_SIZE)
	ptr := db.page.flushed + db.page.nappend
	db.page.nappend++
	db.page.updates[ptr] = node
	return ptr
}

// reuse from freelist or append
func (db *KV) pageAlloc(node []byte) uint64 {
	assert(len(node) == BTREE_PAGE_SIZE)
	if ptr := db.free.PopHead(); ptr != 0 {
		db.page.updates[ptr] = node
		return ptr
	}
	return db.pageAppend(node)
}

func (db *KV) pageWrite(ptr uint64) []byte {
	if node, ok := db.page.updates[ptr]; ok {
		return node
	}
	node := make([]byte, BTREE_PAGE_SIZE)
	copy(node, db.pageReadFile(ptr))
	db.page.updates[ptr] = node
	return node
}

func extendMmap(db *KV, size int) error {
	if size <= db.mmap.total {
		return nil
	}

	alloc := max(db.mmap.total, 64<<20) //double curr address space
	for db.mmap.total+alloc < size {
		alloc *= 2
	}

	chunk, err := syscall.Mmap(
		db.fd, int64(db.mmap.total), alloc,
		syscall.PROT_READ, syscall.MAP_SHARED, // read-only
	)
	if err != nil {
		return fmt.Errorf("mmap: %w", err)
	}

	db.mmap.total += alloc
	db.mmap.chunks = append(db.mmap.chunks, chunk)
	return nil
}

func saveMeta(db *KV) []byte {
	var data [64]byte
	copy(data[:16], []byte(DB_SIG))
	binary.LittleEndian.PutUint64(data[16:24], db.tree.root)
	binary.LittleEndian.PutUint64(data[24:32], db.page.flushed)
	binary.LittleEndian.PutUint64(data[32:40], db.free.headPage)
	binary.LittleEndian.PutUint64(data[40:48], db.free.headSeq)
	binary.LittleEndian.PutUint64(data[48:56], db.free.tailPage)
	binary.LittleEndian.PutUint64(data[56:64], db.free.tailSeq)
	return data[:]
}

func loadMeta(db *KV, data []byte) {
	db.tree.root = binary.LittleEndian.Uint64(data[16:24])
	db.page.flushed = binary.LittleEndian.Uint64(data[24:32])
	db.free.headPage = binary.LittleEndian.Uint64(data[32:40])
	db.free.headSeq = binary.LittleEndian.Uint64(data[40:48])
	db.free.tailPage = binary.LittleEndian.Uint64(data[48:56])
	db.free.tailSeq = binary.LittleEndian.Uint64(data[56:64])
	db.free.maxSeq = db.free.tailSeq // ← NOT in the book. Read the note below.
}

func readRoot(db *KV, fileSize int64) error {
	if fileSize == 0 { // empty file
		db.page.flushed = 2 // the meta page is reserved on the 1st write
		db.free.headPage = 1
		db.free.tailPage = 1
		db.page.updates[1] = make([]byte, BTREE_PAGE_SIZE)
		return nil
	}
	if fileSize%BTREE_PAGE_SIZE != 0 {
		return fmt.Errorf("bad file size: %d", fileSize)
	}

	// read meta page
	data := db.mmap.chunks[0]
	if !bytes.Equal([]byte(DB_SIG), data[:16]) {
		return fmt.Errorf("bad signature")
	}
	loadMeta(db, data)

	maxPages := uint64(fileSize / BTREE_PAGE_SIZE)
	bad := db.page.flushed < 2 || db.page.flushed > maxPages
	bad = bad || db.tree.root >= db.page.flushed
	bad = bad || db.free.headPage == 0 || db.free.headPage >= db.page.flushed
	bad = bad || db.free.tailPage == 0 || db.free.tailPage >= db.page.flushed
	bad = bad || db.free.headSeq > db.free.tailSeq
	if bad {
		return fmt.Errorf("bad meta page")
	}
	return nil
}

// 1. write the newly allocated pages to the end of the file
func writePages(db *KV) error {
	size := (int(db.page.flushed) + int(db.page.nappend)) * BTREE_PAGE_SIZE
	if err := extendMmap(db, size); err != nil {
		return err
	}

	for ptr, page := range db.page.updates {
		assert(len(page) == BTREE_PAGE_SIZE)
		offset := int64(ptr) * BTREE_PAGE_SIZE
		if _, err := syscall.Pwrite(db.fd, page, offset); err != nil {
			return fmt.Errorf("write page %d: %w", ptr, err)
		}
	}
	// discard in-memory data
	db.page.flushed += db.page.nappend
	db.page.nappend = 0
	clear(db.page.updates)
	return nil
}

func updateRoot(db *KV) error {
	if _, err := syscall.Pwrite(db.fd, saveMeta(db), 0); err != nil {
		return fmt.Errorf("write meta page: %w", err)
	}
	return nil
}

func updateFile(db *KV) error {
	if err := writePages(db); err != nil {
		return err
	}
	if err := syscall.Fsync(db.fd); err != nil {
		return err
	}
	// atomic because its only 512 byte sector
	if err := updateRoot(db); err != nil {
		return err
	}

	if err := syscall.Fsync(db.fd); err != nil {
		return err
	}

	db.free.SetMaxSeq()
	return nil
}

func (db *KV) Open() error {
	// reset in case of a reopen
	db.page.updates = map[uint64][]byte{}
	db.page.nappend = 0
	db.failed = false

	// B+tree callbacks
	db.tree.get = db.pageRead      // read a page
	db.tree.new = db.pageAlloc     // (new) reuse from the free list or append
	db.tree.del = db.free.PushTail // (new) freed pages go to the free list
	db.free.get = db.pageRead      // read a page
	db.free.new = db.pageAppend    // append a page
	db.free.set = db.pageWrite     // (new) in-place updates

	fd, err := createFileSync(db.Path)
	if err != nil {
		return err
	}
	db.fd = fd

	var st syscall.Stat_t
	if err = syscall.Fstat(db.fd, &st); err == nil {
		if err = extendMmap(db, int(st.Size)); err == nil {
			err = readRoot(db, st.Size)
		}
	}
	if err != nil {
		db.Close()
		return fmt.Errorf("KV.Open: %w", err)
	}
	return nil
}

func (db *KV) Close() {

	// tears down virtual-address
	//mappings so the process stops referencing the file's pages
	for _, chunk := range db.mmap.chunks {
		if err := syscall.Munmap(chunk); err != nil {
			panic(err)
		}
	}
	db.mmap.chunks = nil
	db.mmap.total = 0
	_ = syscall.Close(db.fd)
	db.fd = -1
}

// Public APIs
func updateOrRevert(db *KV, meta []byte) error {
	// ensure the on-disk meta page matches the in-memory one after an error
	if db.failed {
		if _, err := syscall.Pwrite(db.fd, meta, 0); err != nil {
			return fmt.Errorf("rewrite meta page: %w", err)
		}
		if err := syscall.Fsync(db.fd); err != nil {
			return err
		}
		db.failed = false
	}

	err := updateFile(db)
	if err != nil {
		// the on-disk meta page is in an unknown state;
		// mark it to be rewritten on the next update.
		db.failed = true
		// the in-memory state is reverted immediately to allow reads
		loadMeta(db, meta)
		// discard temporaries
		clear(db.page.updates)
		db.page.nappend = 0
	}
	return err
}

// read key
func (db *KV) Get(key []byte) ([]byte, bool) {
	return db.tree.Get(key)
}

// insert or update key
func (db *KV) Set(key []byte, val []byte) error {
	meta := saveMeta(db)
	if err := db.tree.Insert(key, val); err != nil {
		return err
	}
	return updateOrRevert(db, meta)
}

// del a key
func (db *KV) Del(key []byte) (bool, error) {
	meta := saveMeta(db)
	deleted, err := db.tree.Delete(key)
	if err != nil || !deleted {
		return false, err // length limit or not found; nothing changed
	}
	return true, updateOrRevert(db, meta)
}
