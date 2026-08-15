package main

func assert(cond bool) {
	if !cond {
		panic("assertion failure")
	}
}

const BTREE_PAGE_SIZE = 4096
const BTREE_MAX_KEY_SIZE = 1000
const BTREE_MAX_VAL_SIZE = 3000
const HEADER = 4

const (
	BNODE_NODE = 1 // internal nodes with pointers
	BNODE_LEAF = 2 // leaf nodes with values
)


func init() {
	node1max := 4 + 1*8 + 1*2 + 4 + BTREE_MAX_KEY_SIZE + BTREE_MAX_VAL_SIZE
	assert(node1max <= BTREE_PAGE_SIZE)
}