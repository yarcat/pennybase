package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testData(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
	if err != nil {
		t.Fatalf("Failed to copy testdata: %v", err)
	}
	return dst
}

func TestStoreCRUD(t *testing.T) {
	originalID := ID
	defer func() { ID = originalID }()

	tests := []struct {
		name      string
		operation func(*Store) error
		wantErr   bool
		postCheck func(*Store) error
	}{
		{
			name: "Create valid book",
			operation: func(s *Store) error {
				ID = func() string { return "test-id-1" }
				_, err := s.Create("books", Resource{
					"title":            "The Go Programming Language",
					"author":           "Alan Donovan",
					"publication_year": 2015.0,
					"genres":           []string{"Programming"},
					"isbn":             "123-0123456789",
				})
				return err
			},
			wantErr: false,
			postCheck: func(s *Store) error {
				res, err := s.Get("books", "test-id-1")
				if err != nil {
					return err
				}
				if res["title"] != "The Go Programming Language" || res["_v"].(float64) != 1.0 {
					return errors.New("created book mismatch")
				}
				return nil
			},
		},
		{
			name: "Create invalid book (missing title)",
			operation: func(s *Store) error {
				_, err := s.Create("books", Resource{
					"author":           "Anonymous",
					"publication_year": 2023.0,
					"genres":           []string{"Mystery"},
					"isbn":             "999-9999999999",
				})
				return err
			},
			wantErr: true,
		},
		{
			name: "Update book",
			operation: func(s *Store) error {
				ID = func() string { return "test-id-2" }
				_, err := s.Create("books", Resource{
					"title":            "Original Title",
					"author":           "Author",
					"publication_year": 2020.0,
					"genres":           []string{"Old"},
					"isbn":             "111-1111111111",
				})
				if err != nil {
					return err
				}
				return s.Update("books", Resource{
					"_id":              "test-id-2",
					"title":            "Updated Title",
					"author":           "Author",
					"publication_year": 2020.0,
					"genres":           []string{"New"},
					"isbn":             "111-1111111111",
				})
			},
			wantErr: false,
			postCheck: func(s *Store) error {
				res, err := s.Get("books", "test-id-2")
				if err != nil {
					return err
				}
				if res["title"] != "Updated Title" || res["_v"].(float64) != 2.0 {
					return errors.New("update failed")
				}
				return nil
			},
		},
		{
			name: "Delete book",
			operation: func(s *Store) error {
				ID = func() string { return "test-id-3" }
				_, err := s.Create("books", Resource{
					"title":            "To Delete",
					"author":           "Author",
					"publication_year": 2021.0,
					"genres":           []string{"Temp"},
					"isbn":             "333-3333333333",
				})
				if err != nil {
					return err
				}
				return s.Delete("books", "test-id-3")
			},
			wantErr: false,
			postCheck: func(s *Store) error {
				_, err := s.Get("books", "test-id-3")
				if err == nil {
					return errors.New("book not deleted")
				}
				return nil
			},
		},
		{
			name: "List books sorted",
			operation: func(s *Store) error {
				ID = func() string { return "book1" }
				_, err := s.Create("books", Resource{
					"title":            "Book A",
					"author":           "Author A",
					"publication_year": 2000.0,
					"genres":           []string{"Genre A"},
					"isbn":             "111-0000000000",
				})
				if err != nil {
					return err
				}
				ID = func() string { return "book2" }
				_, err = s.Create("books", Resource{
					"title":            "Book B",
					"author":           "Author B",
					"publication_year": 2020.0,
					"genres":           []string{"Genre B"},
					"isbn":             "222-0000000000",
				})
				return err
			},
			wantErr: false,
			postCheck: func(s *Store) error {
				books, err := s.List("books", "publication_year")
				if err != nil || len(books) != 2 {
					return errors.New("list failed")
				}
				if books[0]["publication_year"].(float64) != 2000.0 || books[1]["publication_year"].(float64) != 2020.0 {
					return errors.New("incorrect sort order")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := testData(t, "testdata/basic")
			store, err := NewStore(dir)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			err = tt.operation(store)
			if (err != nil) != tt.wantErr {
				t.Errorf("got error %v, wantErr %v", err, tt.wantErr)
			}
			if tt.postCheck != nil {
				if err := tt.postCheck(store); err != nil {
					t.Errorf("postCheck failed: %v", err)
				}
			}
		})
	}
}
