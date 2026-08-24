package main

import (
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

	mmap struct {
		total  int
		chunks [][]byte
	}
	page struct {
		flushed uint64   // database size in number of pages
		temp    [][]byte // newly allocated pages, not yet on disk
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
func (db *KV) pageRead(ptr uint64) []byte {
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

// add a new page, written to disk later. return its page number
func (db *KV) pageAppend(node []byte) uint64 {
	assert(len(node) == BTREE_PAGE_SIZE)
	ptr := db.page.flushed + uint64(len(db.page.temp))
	db.page.temp = append(db.page.temp, node)
	return ptr
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
