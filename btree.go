package main

import (
	"bytes"
)


type BTree struct {
	root uint64
	get func(uint64) []byte 
	new func([]byte) uint64
	del func(uint64)
}


func treeInsert(tree *BTree, node BNode, key []byte, val []byte) BNode {

	new := BNode(make([]byte, 2*BTREE_PAGE_SIZE))

	// find where to insert
	idx := nodeLookupLE(node, key) // first getKey(idx) <= key

	switch node.btype() {
	case BNODE_LEAF:
		if bytes.Equal(key, node.getKey(idx)){
			leafUpdate(new, node, idx, key, val)
		} else {
			leafInsert(new, node, idx+1, key, val) 
		}
	case BNODE_NODE:
		// recursive insertion into kid node
		kptr := node.getPtr(idx)
		knode := treeInsert(tree, tree.get(kptr), key, val)
		
		// after insertion, split the result
		nsplit, split := nodeSplit3(knode)
		tree.del(kptr)
		
		nodeReplaceKidN(tree, new, node, idx, split[:nsplit]...)

	default:
		panic("bad node!")
	}

	return new
}

func nodeReplaceKidN(
	tree *BTree, new BNode, old BNode, idx uint16, kids ...BNode,
) {
	inc := uint16(len(kids))
	new.setHeader(BNODE_NODE, old.nkeys()+inc-1)
	nodeAppendRange(new, old, 0, 0, idx)
	for i, node := range kids {
		nodeAppendKV(new, idx+uint16(i), tree.new(node), node.getKey(0), nil)
	}
	nodeAppendRange(new, old, idx+inc, idx+1, old.nkeys()-(idx+1))
}
