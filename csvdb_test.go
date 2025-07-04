package pennybase

import (
	"crypto/rand"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
)

var _ DB = (*csvDB)(nil)

func TestDBBasicOperations(t *testing.T) {
	db := must(NewCSVDB(filepath.Join(t.TempDir(), "test.csv"))).T(t)
	defer db.Close()

	id := rand.Text()
	if rec, err := db.Get(id); err == nil {
		t.Fatalf("want not record, got %v", rec)
	}

	initialRec := Record{id, "1", "foo"}
	must0(t, db.Create(initialRec))

	if rec := must(db.Get(id)).T(t); !slices.Equal(rec, initialRec) {
		t.Fatalf("get after create got %v, want %v", rec, initialRec)
	}

	updatedRec := Record{id, "2", "bar"}
	must0(t, db.Update(updatedRec))

	if rec := must(db.Get(id)).T(t); !slices.Equal(rec, updatedRec) {
		t.Fatalf("get after update got %v, want %v", rec, updatedRec)
	}

	if err := db.Update(updatedRec); err == nil {
		t.Fatal("want error on same value update")
	}

	must0(t, db.Delete(id))

	if rec, err := db.Get(id); err == nil {
		t.Fatalf("got unexpected record after delete: %v", rec)
	}

	if err := db.Update(Record{id, "3", "qux"}); err == nil {
		t.Fatal("want error on update after delete")
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
	db := must(NewCSVDB(filepath.Join(t.TempDir(), "test.csv"))).T(t)
	defer db.Close()

	for i := range 10 {
		must0(t, db.Create(Record{strconv.Itoa(i), "1", "data"}))
		must0(t, db.Delete(strconv.Itoa(i)))
	}
	must0(t, db.Create(Record{"active", "1", "data"}))

	count := 0
	for r := range db.Iter() {
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
	db := must(NewCSVDB(filepath.Join(t.TempDir(), "test.csv"))).T(t)
	defer db.Close()
	var wg sync.WaitGroup
	for i := range 1000 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := strconv.Itoa(i)
			must0(t, db.Create(Record{id, "1", "data"}))
			must(db.Get(id)).T(t)
		}()
	}
	wg.Wait()
	for i := range 1000 {
		id := strconv.Itoa(i)
		must(db.Get(id)).T(t)
	}
}

func BenchmarkWrite(b *testing.B) {
	db, _ := NewCSVDB(filepath.Join(b.TempDir(), "test.csv"))
	defer db.Close()
	b.ResetTimer()
	for i := range b.N {
		id := strconv.Itoa(i)
		_ = db.Create(Record{id, "1", "data"})
	}
}

func BenchmarkRead(b *testing.B) {
	db, _ := NewCSVDB(filepath.Join(b.TempDir(), "test.csv"))
	defer db.Close()
	for i := range 1000 {
		id := strconv.Itoa(i)
		_ = db.Create(Record{id, "1", "data"})
	}
	b.ResetTimer()
	for i := range b.N {
		id := strconv.Itoa(i % 1000)
		_, _ = db.Get(id)
	}
}
