package main

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

const testID = "test0001"

func TestFieldSchema(t *testing.T) {
	tests := []struct {
		name     string
		field    FieldSchema
		value    any
		expected bool
	}{
		// Number validation
		{
			name:     "valid number within range",
			field:    FieldSchema{Type: Number, Min: 5, Max: 10},
			value:    7.0,
			expected: true,
		},
		{
			name:     "number at min boundary",
			field:    FieldSchema{Type: Number, Min: 5, Max: 10},
			value:    5.0,
			expected: true,
		},
		{
			name:     "number at max boundary",
			field:    FieldSchema{Type: Number, Min: 5, Max: 10},
			value:    10.0,
			expected: true,
		},
		{
			name:     "number below min",
			field:    FieldSchema{Type: Number, Min: 5, Max: 10},
			value:    4.9,
			expected: false,
		},
		{
			name:     "number above max",
			field:    FieldSchema{Type: Number, Min: 5, Max: 10},
			value:    10.1,
			expected: false,
		},
		{
			name:     "invalid number type",
			field:    FieldSchema{Type: Number},
			value:    "not a number",
			expected: false,
		},

		// Text validation
		{
			name:     "text matches regex",
			field:    FieldSchema{Type: Text, Regex: "^[a-z]+$"},
			value:    "lowercase",
			expected: true,
		},
		{
			name:     "text doesn't match regex",
			field:    FieldSchema{Type: Text, Regex: "^[a-z]+$"},
			value:    "Uppercase",
			expected: false,
		},
		{
			name:     "empty text with regex",
			field:    FieldSchema{Type: Text, Regex: "^.*$"},
			value:    "",
			expected: true,
		},

		// List validation
		{
			name:     "valid string list",
			field:    FieldSchema{Type: List},
			value:    []string{"a", "b"},
			expected: true,
		},
		{
			name:     "empty list",
			field:    FieldSchema{Type: List},
			value:    []string{},
			expected: true,
		},
		{
			name:     "invalid list type",
			field:    FieldSchema{Type: List},
			value:    "not a list",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expected != tt.field.Validate(tt.value) {
				t.Errorf("expected %v, got %v", tt.expected, tt.field.Validate(tt.value))
			}
		})
	}
}

func TestSchemaRecordConversion(t *testing.T) {
	testSchema := Schema{
		{Field: "_id", Type: Text, Regex: "^[A-Za-z0-9]+$"},
		{Field: "_v", Type: Number, Min: 1},
		{Field: "name", Type: Text, Regex: "^[A-Z][a-z]*$"},
		{Field: "age", Type: Number, Min: 0, Max: 150},
		{Field: "tags", Type: List},
	}

	tests := []struct {
		name        string
		resource    Resource
		expectedRec Record
		expectErr   bool
	}{
		{
			name: "valid complete resource",
			resource: Resource{
				"_id":  testID,
				"_v":   1.0,
				"name": "John",
				"age":  30.0,
				"tags": []string{"admin", "user"},
			},
			expectedRec: Record{
				testID,
				"1",
				"John",
				"30",
				"admin,user",
			},
			expectErr: false,
		},
		{
			name: "missing optional fields",
			resource: Resource{
				"_id":  testID,
				"_v":   1.0,
				"name": "John",
			},
			expectedRec: Record{
				testID,
				"1",
				"John",
				"0",
				"",
			},
			expectErr: false,
		},
		{
			name: "invalid _id format",
			resource: Resource{
				"_id": "?",
				"_v":  1.0,
			},
			expectErr: true,
		},
		{
			name: "invalid name regex",
			resource: Resource{
				"_id":  testID,
				"_v":   1.0,
				"name": "john", // lowercase
				"age":  30.0,
			},
			expectErr: true,
		},
		{
			name: "age out of range",
			resource: Resource{
				"_id": testID,
				"_v":  1.0,
				"age": 200.0,
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := testSchema.Record(tt.resource)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				return
			} else if err != nil {
				t.Errorf("unexpected error: %v, resource: %v", err, tt.resource)
			} else if len(rec) != len(tt.expectedRec) {
				t.Errorf("expected record length %d, got %d", len(tt.expectedRec), len(rec))
			} else {
				for i, v := range rec {
					if v != tt.expectedRec[i] {
						t.Errorf("expected %v, got %v", tt.expectedRec[i], v)
					}
				}
			}
		})
	}
}

func TestSchemaResourceConversion(t *testing.T) {
	testSchema := Schema{
		{Field: "_id", Type: Text, Regex: "^[A-Za-z0-9]+$"},
		{Field: "_v", Type: Number, Min: 1},
		{Field: "name", Type: Text},
		{Field: "age", Type: Number},
		{Field: "tags", Type: List},
	}

	tests := []struct {
		name        string
		record      Record
		expectedRes Resource
		expectErr   bool
	}{
		{
			name:   "valid complete record",
			record: Record{testID, "2", "Alice", "25", "staff,manager"},
			expectedRes: Resource{
				"_id":  testID,
				"_v":   2.0,
				"name": "Alice",
				"age":  25.0,
				"tags": []string{"staff", "manager"},
			},
			expectErr: false,
		},
		{
			name:      "invalid record length",
			record:    Record{"ID", "1", "extra"},
			expectErr: true,
		},
		{
			name:      "invalid version format",
			record:    Record{testID, "invalid", "Alice", "25"},
			expectErr: true,
		},
		{
			name:      "invalid number format",
			record:    Record{testID, "1", "Alice", "notanumber"},
			expectErr: true,
		},
		{
			name:   "empty list field",
			record: Record{testID, "1", "Bob", "40", ""},
			expectedRes: Resource{
				"_id":  testID,
				"_v":   1.0,
				"name": "Bob",
				"age":  40.0,
				"tags": []string{},
			},
			expectErr: false,
		},
		{
			name:   "single list item",
			record: Record{testID, "1", "Charlie", "35", "admin"},
			expectedRes: Resource{
				"_id":  testID,
				"_v":   1.0,
				"name": "Charlie",
				"age":  35.0,
				"tags": []string{"admin"},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := testSchema.Resource(tt.record)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				return
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			} else if len(res) != len(tt.expectedRes) {
				t.Errorf("expected resource length %d, got %d", len(tt.expectedRes), len(res))
			} else {
				for k, v := range res {
					if !reflect.DeepEqual(v, tt.expectedRes[k]) {
						t.Errorf("expected %#v, got %#v", tt.expectedRes[k], v)
					}
				}
			}
		})
	}
}

func TestSchema_EdgeCases(t *testing.T) {
	t.Run("empty resource", func(t *testing.T) {
		schema := Schema{}
		resource := Resource{}

		rec, err := schema.Record(resource)
		if err != nil {
			t.Fatal(err)
		}
		if len(rec) != 0 {
			t.Fatal(rec)
		}
		res, err := schema.Resource(rec)
		if err != nil {
			t.Fatal(err)
		}
		if len(res) != 0 {
			t.Fatal(res)
		}
	})

	t.Run("large numbers", func(t *testing.T) {
		schema := Schema{{Field: "big", Type: Number}}
		resource := Resource{"big": math.MaxFloat64}

		rec, err := schema.Record(resource)
		if err != nil {
			t.Fatal(err)
		}
		if len(rec) != 1 || rec[0] != fmt.Sprintf("%g", math.MaxFloat64) {
			t.Fatal(rec)
		}
	})

	t.Run("special characters in text", func(t *testing.T) {
		schema := Schema{{Field: "text", Type: Text}}
		resource := Resource{"text": "特殊字符 日本語" }

		rec, err := schema.Record(resource)
		if err != nil {
			t.Fatal(err)
		}
		if len(rec) != 1 || rec[0] != "特殊字符 日本語" {
			t.Fatal(rec)
		}
	})
}
