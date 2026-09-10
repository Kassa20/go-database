package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func newBenchDB(b *testing.B) *KV {
	b.Helper()
	db := &KV{Path: filepath.Join(b.TempDir(), "bench.db")}
	if err := db.Open(); err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(db.Close)
	return db
}

func benchKey(i int) []byte {
	return []byte(fmt.Sprintf("key%08d", i))
}

func BenchmarkSet(b *testing.B) {
	db := newBenchDB(b)
	val := []byte("value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.Set(benchKey(i), val); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDel(b *testing.B) {
	db := newBenchDB(b)
	val := []byte("value")
	for i := 0; i < b.N; i++ {
		if err := db.Set(benchKey(i), val); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer() // exclude the setup inserts
	for i := 0; i < b.N; i++ {
		if _, err := db.Del(benchKey(i)); err != nil {
			b.Fatal(err)
		}
	}
}
