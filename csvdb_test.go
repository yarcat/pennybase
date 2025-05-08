package main

import (
	"crypto/rand"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

var _ DB = (*csvDB)(nil)

func TestDBBasicOperations(t *testing.T) {
	db, err := NewCSVDB(filepath.Join(t.TempDir(), "test.csv"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	id := rand.Text()
	// Does not exist yet
	if _, err := db.Get(id); err == nil {
		t.Fatal()
	}
	// Create a new record
	if err := db.Create(Record{id, "1", "foo"}); err != nil {
		t.Fatal()
	}
	// Get the record
	if rec, err := db.Get(id); err != nil || len(rec) != 3 || rec[0] != id || rec[1] != "1" || rec[2] != "foo" {
		t.Fatal(err)
	}
	// Update the record
	if err := db.Update(Record{id, "2", "bar"}); err != nil {
		t.Fatal(err)
	}
	// Check the updated record
	if rec, err := db.Get(id); err != nil || len(rec) != 3 || rec[0] != id || rec[1] != "2" || rec[2] != "bar" {
		t.Fatal(err)
	}
	// Optimistic concurrency control
	if err := db.Update(Record{id, "2", "baz"}); err == nil {
		t.Fatal("expected error")
	}
	// Delete the record
	if err := db.Delete(id); err != nil {
		t.Fatal(err)
	}
	// Try to get the record
	if _, err := db.Get(id); err == nil {
		t.Fatal(err)
	}
	// Try to update the record
	if err := db.Update(Record{id, "3", "qux"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestEmptyIterator(t *testing.T) {
	db, _ := NewCSVDB(filepath.Join(t.TempDir(), "test.csv"))
	defer db.Close()
	count := 0
	for range db.Iter() {
		count++
	}
	if count != 0 {
		t.Fatalf("Expected 0 records, got %d", count)
	}
}

func TestIteratorWithDeletes(t *testing.T) {
	db, _ := NewCSVDB(filepath.Join(t.TempDir(), "test.csv"))
	defer db.Close()
	for i := 0; i < 10; i++ {
		_ = db.Create(Record{strconv.Itoa(i), "1", "data"})
		_ = db.Delete(strconv.Itoa(i))
	}
	_ = db.Create(Record{"active", "1", "data"})

	count := 0
	for r, _ := range db.Iter() {
		if r[0] == "active" {
			count++
		} else if r[1] != "0" {
			t.Errorf("Unexpected record: %v", r)
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 active record, got %d", count)
	}
}

func TestConcurrent(t *testing.T) {
	db, err := NewCSVDB(filepath.Join(t.TempDir(), "test.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := strconv.Itoa(i)
			if err := db.Create(Record{id, "1", "data"}); err != nil {
				t.Error(err)
			}
			if _, err := db.Get(id); err != nil {
				t.Error("Exists failed", err)
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 1000; i++ {
		id := strconv.Itoa(i)
		if _, err := db.Get(id); err != nil {
			t.Fatal("Missing record", id, err)
		}
	}
}

func BenchmarkWrite(b *testing.B) {
	db, _ := NewCSVDB(filepath.Join(b.TempDir(), "test.csv"))
	defer db.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := strconv.Itoa(i)
		_ = db.Create(Record{id, "1", "data"})
	}
}

func BenchmarkRead(b *testing.B) {
	db, _ := NewCSVDB(filepath.Join(b.TempDir(), "test.csv"))
	defer db.Close()
	for i := 0; i < 1000; i++ {
		id := strconv.Itoa(i)
		_ = db.Create(Record{id, "1", "data"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := strconv.Itoa(i % 1000)
		_, _ = db.Get(id)
	}
}
