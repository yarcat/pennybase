package pennybase

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerREST(t *testing.T) {
	testDir := testData(t, "testdata/rest")
	s, err := NewServer(
		testDir,
		filepath.Join(testDir, "templates"),
		filepath.Join(testDir, "static"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Close()

	ts := httptest.NewServer(s)
	defer ts.Close()

	tests := []struct {
		name     string
		method   string
		path     string
		body     any
		auth     [2]string // user, pass
		status   int
		validate func(*testing.T, *http.Response)
	}{
		// Public endpoints
		{
			name:   "List books unauthorized",
			method: "GET",
			path:   "/api/books/",
			status: http.StatusOK,
			validate: func(t *testing.T, resp *http.Response) {
				var books []Resource
				json.NewDecoder(resp.Body).Decode(&books)
				if len(books) != 2 {
					t.Errorf("Expected 2 books, got %d", len(books))
				}
			},
		},
		{
			name:   "Get static file",
			method: "GET",
			path:   "/static/test.txt",
			status: http.StatusOK,
			validate: func(t *testing.T, resp *http.Response) {
				body, _ := io.ReadAll(resp.Body)
				if string(body) != "Static text file\n" {
					t.Errorf("Static file content mismatch: %s", string(body))
				}
			},
		},

		// Template rendering
		{
			name:   "Render books template",
			method: "GET",
			path:   "/books.html",
			status: http.StatusOK,
			validate: func(t *testing.T, resp *http.Response) {
				body, _ := io.ReadAll(resp.Body)
				if !strings.Contains(string(body), "The Go Programming Language") ||
					!strings.Contains(string(body), "1984") {
					t.Error(string(body))
					t.Error("Template missing book data")
				}
			},
		},

		// Authentication tests
		{
			name:   "Create book unauthenticated",
			method: "POST",
			path:   "/api/books/",
			body:   Resource{"title": "New Book"},
			status: http.StatusUnauthorized,
		},
		{
			name:   "Create book invalid credentials",
			method: "POST",
			path:   "/api/books/",
			body:   Resource{"title": "New Book"},
			auth:   [2]string{"user1", "wrongpass"},
			status: http.StatusUnauthorized,
		},

		// Authorized operations
		{
			name:   "Create book valid user",
			method: "POST",
			path:   "/api/books/",
			body:   Resource{"title": "Valid Book", "author": "Unknown Author", "year": 2023},
			auth:   [2]string{"user1", "user1pass"},
			status: http.StatusCreated,
			validate: func(t *testing.T, resp *http.Response) {
				loc := resp.Header.Get("Location")
				if !strings.HasPrefix(loc, "/api/books/") {
					t.Error("Missing Location header")
				}
			},
		},
		{
			name:   "Update book unauthorized",
			method: "PUT",
			path:   "/api/books/book1",
			body:   Resource{"title": "Updated Title"},
			auth:   [2]string{"user1", "user1pass"},
			status: http.StatusUnauthorized,
		},

		// Admin operations
		{
			name:   "Delete book as admin",
			method: "DELETE",
			path:   "/api/books/book2",
			auth:   [2]string{"admin", "admin123"},
			status: http.StatusOK,
		},

		// Validation tests
		{
			name:   "Create invalid book",
			method: "POST",
			path:   "/api/books/",
			body:   Resource{"title": "Book 123", "year": 3000},
			auth:   [2]string{"user1", "user1pass"},
			status: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != nil {
				b, _ := json.Marshal(tc.body)
				body = bytes.NewReader(b)
			}

			req, _ := http.NewRequest(tc.method, ts.URL+tc.path, body)
			if tc.auth[0] != "" {
				req.SetBasicAuth(tc.auth[0], tc.auth[1])
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.status {
				t.Errorf("Expected status %d, got %d", tc.status, resp.StatusCode)
			}

			if tc.validate != nil {
				tc.validate(t, resp)
			}
		})
	}
}

func TestServerTemplate(t *testing.T) {
	dir := testData(t, "testdata/rest")
	s, _ := NewServer(dir, filepath.Join(dir, "templates"), "")
	req := httptest.NewRequest("GET", "/books.html", nil)
	w := httptest.NewRecorder()
	s.Mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK: %v", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<div class=\"book\">The Go Programming Language (2015)</div>") {
		t.Errorf("Template rendering failed: %v", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<div class=\"book\">1984 (1949)</div>") {
		t.Errorf("Template rendering failed: %v", w.Body.String())
	}
}

func TestServerStaticFiles(t *testing.T) {
	dir := testData(t, "testdata/rest")
	s, _ := NewServer(dir, "", filepath.Join(dir, "static"))
	req := httptest.NewRequest("GET", "/static/test.txt", nil)
	w := httptest.NewRecorder()
	s.Mux.ServeHTTP(w, req)
	if w.Body.String() != "Static text file\n" {
		t.Errorf("Static file serving failed: %v", w.Body.String())
	}
}
