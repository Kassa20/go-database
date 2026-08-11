package main

import "fmt"


func main() {
	node := BNode(make([]byte, BTREE_PAGE_SIZE))
	node.setHeader(BNODE_LEAF, 2)

	nodeAppendKV(node, 0, 0, []byte("key1"), []byte("hello"))
	nodeAppendKV(node, 1, 0, []byte("key2"), []byte("world"))

	fmt.Println("nkeys:", node.nkeys())

	fmt.Printf("key 0 = %s, val 0 = %s\n", node.getKey(0), node.getVal(0))
	fmt.Printf("key 1 = %s, val 1 = %s\n", node.getKey(1), node.getVal(1))
	fmt.Println("nbytes:", node.nbytes())

}
