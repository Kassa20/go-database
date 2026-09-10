package main

import "encoding/binary"

// node format:
// | next | pointers | unused |
// |  8B  |   n*8B   |   ...  |
type LNode []byte

const FREE_LIST_HEADER = 8
const FREE_LIST_CAP = (BTREE_PAGE_SIZE - FREE_LIST_HEADER) / 8

func (node LNode) getNext() uint64 {
	return binary.LittleEndian.Uint64(node[0:8])
}

func (node LNode) setNext(next uint64) {
	binary.LittleEndian.PutUint64(node[0:8], next)
}

func (node LNode) getPtr(idx int) uint64 {
	assert(0 <= idx && idx < FREE_LIST_CAP)
	offset := FREE_LIST_HEADER + 8*idx
	return binary.LittleEndian.Uint64(node[offset : offset+8])
}

func (node LNode) setPtr(idx int, ptr uint64) {
	assert(0 <= idx && idx < FREE_LIST_CAP)
	offset := FREE_LIST_HEADER + 8*idx
	binary.LittleEndian.PutUint64(node[offset:offset+8], ptr)
}

type FreeList struct {
	get func(uint64) []byte
	new func([]byte) uint64
	set func(uint64) []byte

	headPage uint64
	headSeq  uint64
	tailPage uint64
	tailSeq  uint64

	maxSeq uint64
}

func seq2idx(seq uint64) int {
	return int(seq % FREE_LIST_CAP)
}

func (fl *FreeList) SetMaxSeq() {
	fl.maxSeq = fl.tailSeq
}

func flPop(fl *FreeList) (ptr uint64, head uint64) {
	if fl.headSeq == fl.maxSeq {
		return 0, 0
	}
	node := LNode(fl.get(fl.headPage))
	ptr = node.getPtr(seq2idx(fl.headSeq))
	fl.headSeq++
	if seq2idx(fl.headSeq) == 0 {
		head, fl.headPage = fl.headPage, node.getNext()
		assert(fl.headPage != 0)
	}
	return
}

func (fl *FreeList) PopHead() uint64 {
	ptr, head := flPop(fl)
	if head != 0 { // the empty head node is recycled
		fl.PushTail(head)
	}
	return ptr
}

func (fl *FreeList) PushTail(ptr uint64) {
	LNode(fl.set(fl.tailPage)).setPtr(seq2idx(fl.tailSeq), ptr)
	fl.tailSeq++
	if seq2idx(fl.tailSeq) == 0 {
		next, head := flPop(fl)
		if next == 0 {
			next = fl.new(make([]byte, BTREE_PAGE_SIZE))
		}
		LNode(fl.set(fl.tailPage)).setNext(next)
		fl.tailPage = next
		// also add the head node if it's removed
		if head != 0 {
			LNode(fl.set(fl.tailPage)).setPtr(0, head)
			fl.tailSeq++
		}
	}
}
