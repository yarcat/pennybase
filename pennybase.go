package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Record []string
type Resource map[string]any
type FieldType string

const (
	Number FieldType = "number"
	Text   FieldType = "text"
	List   FieldType = "list"
)

type FieldSchema struct {
	Resource string
	Field    string
	Type     FieldType
	Min      float64
	Max      float64
	Regex    string
}

type Schema []FieldSchema

func (field FieldSchema) Validate(v any) bool {
	if v == nil {
		return false
	}
	switch field.Type {
	case Number:
		n, ok := v.(float64)
		return ok && ((field.Min == 0 && field.Max == 0) || (n >= field.Min && (field.Max < field.Min || n <= field.Max)))
	case Text:
		s, ok := v.(string)
		return ok && (field.Regex == "" || regexp.MustCompile(field.Regex).MatchString(s))
	case List:
		_, ok := v.([]string)
		return ok
	}
	return false
}

func (s Schema) Record(res Resource) (Record, error) {
	rec := Record{}
	for _, field := range s {
		v := res[field.Field]
		if v == nil {
			v = map[FieldType]any{Number: 0.0, Text: "", List: []string{}}[field.Type]
		}
		if !field.Validate(v) {
			return nil, fmt.Errorf("invalid field \"%s\"", field.Field)
		}
		switch field.Type {
		case Number:
			rec = append(rec, fmt.Sprintf("%g", v))
		case Text:
			rec = append(rec, v.(string))
		case List:
			rec = append(rec, strings.Join(v.([]string), ","))
		}
	}
	return rec, nil
}

func (s Schema) Resource(rec Record) (Resource, error) {
	res := Resource{}
	for i, field := range s {
		if i >= len(rec) {
			return nil, fmt.Errorf("record length %d is less than schema length %d", len(rec), len(s))
		}
		switch field.Type {
		case Number:
			n, err := strconv.ParseFloat(rec[i], 64)
			if err != nil {
				return nil, err
			}
			res[field.Field] = n
		case Text:
			res[field.Field] = rec[i]
		case List:
			if rec[i] != "" {
				res[field.Field] = strings.Split(rec[i], ",")
			} else {
				res[field.Field] = []string{}
			}
		default:
			return nil, fmt.Errorf("unknown field type %s", field.Type)
		}
	}
	return res, nil
}

func main() {}
