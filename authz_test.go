package pennybase

import (
	"errors"
	"testing"
)

func TestAuthorization(t *testing.T) {
	tests := []struct {
		name        string
		resource    string
		id          string
		action      string
		username    string
		password    string
		wantErr     bool
		expectedErr error
	}{
		{
			name:     "Public read access",
			resource: "books",
			action:   "read",
			username: "",
			password: "",
			wantErr:  false,
		},
		{
			name:     "Create with editor role",
			resource: "books",
			action:   "create",
			username: "alice",
			password: "alicepass",
			wantErr:  false,
		},
		{
			name:     "Update own post via owner field",
			resource: "books",
			id:       "book123",
			action:   "update",
			username: "bob",
			password: "bobpass",
			wantErr:  false,
		},
		{
			name:        "Delete without admin role",
			resource:    "books",
			action:      "delete",
			username:    "alice",
			password:    "alicepass",
			wantErr:     true,
			expectedErr: errors.New("unauthorized"),
		},
		{
			name:     "Full access via owner field as a list",
			resource: "books",
			action:   "update",
			id:       "book123",
			username: "alice",
			password: "alicepass",
			wantErr:  false,
		},
		{
			name:        "Invalid credentials",
			resource:    "books",
			action:      "create",
			username:    "alice",
			password:    "wrongpass",
			wantErr:     true,
			expectedErr: errors.New("unauthicated"),
		},
		{
			name:     "Admin delete access",
			resource: "books",
			action:   "delete",
			username: "admin",
			password: "admin123",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := testData(t, "testdata/authz")
			store, err := NewStore(dir)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			defer store.Close()

			// Setup test resource if needed
			// if tt.id != "" {
			// 	store.Create("books", Resource{
			// 		"_id":    tt.id,
			// 		"title":  "Test Book",
			// 		"owner":  tt.username,
			// 		"admins": []string{tt.username},
			// 	})
			// }
			//
			u, _ := store.AuthenticateBasic(tt.username, tt.password)
			err = store.Authorize(tt.resource, tt.id, tt.action, u)

			if (err != nil) != tt.wantErr {
				t.Errorf("Authorize() error = %v, wantErr %v", err, tt.wantErr)
			}
			// if tt.expectedErr != nil && !errors.Is(err, tt.expectedErr) {
			// 	t.Errorf("Expected error %v, got %v", tt.expectedErr, err)
			// }
		})
	}
}
