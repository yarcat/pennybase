package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

type DB interface {
	Create(r Record) error
	Update(r Record) error
	Get(id string) (Record, error)
	Delete(id string) error
	Iter() func(yield func(Record, error) bool)
	Close() error
}

var ID = func() string { return rand.Text() }

var Salt = func() string { return rand.Text() }
var HashPasswd = func(passwd, salt string) string {
	sum := sha256.Sum256([]byte(salt + passwd))
	return base32.StdEncoding.EncodeToString(sum[:])
}

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

type csvDB struct {
	mu      sync.Mutex
	f       *os.File
	w       *csv.Writer
	index   map[string]int64
	version map[string]int64
}

func NewCSVDB(path string) (*csvDB, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	db := &csvDB{f: f, w: csv.NewWriter(f), index: map[string]int64{}, version: map[string]int64{}}
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	for {
		pos := r.InputOffset()
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) > 0 {
			db.index[rec[0]] = pos
			db.version[rec[0]], _ = strconv.ParseInt(rec[1], 10, 64)
		}
	}
	return db, nil
}

func (db *csvDB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.w.Flush()
	return db.f.Close()
}

func (db *csvDB) append(r Record) error {
	pos, _ := db.f.Seek(0, io.SeekEnd)
	err := db.w.Write(r)
	if err != nil {
		return err
	}
	db.w.Flush()
	db.index[r[0]] = pos
	db.version[r[0]], err = strconv.ParseInt(r[1], 10, 64)
	return err
}

func (db *csvDB) Create(r Record) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if r[0] == "" || r[1] != "1" || db.version[r[0]] != 0 {
		return errors.New("invalid record")
	}
	return db.append(r)
}

func (db *csvDB) Update(r Record) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if len(r) == 0 || r[1] != strconv.FormatInt(db.version[r[0]]+1, 10) {
		return errors.New("invalid record version")
	}
	return db.append(r)
}

func (db *csvDB) Delete(id string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.version[id] < 1 {
		return errors.New("record not found")
	}
	return db.append(Record{id, "0"})
}

func (db *csvDB) Get(id string) (Record, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.version[id] < 1 {
		return nil, errors.New("record not found")
	}
	offset, ok := db.index[id]
	if !ok {
		return nil, nil
	}
	if _, err := db.f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	r := csv.NewReader(db.f)
	rec, err := r.Read()
	if err != nil {
		return nil, err
	}
	if len(rec) > 0 && rec[0] != id {
		log.Println(rec)
		return nil, errors.New("corrupted index")
	}
	return rec, nil
}

func (db *csvDB) Iter() func(yield func(Record, error) bool) {
	return func(yield func(Record, error) bool) {
		db.mu.Lock()
		defer db.mu.Unlock()
		if _, err := db.f.Seek(0, io.SeekStart); err != nil {
			yield(nil, err)
			return
		}
		r := csv.NewReader(db.f)
		r.FieldsPerRecord = -1
		for {
			rec, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				yield(nil, err)
				return
			}
			if len(rec) < 2 {
				continue
			}
			id, version := rec[0], rec[1]
			if version == "0" || version != strconv.FormatInt(db.version[id], 10) {
				continue // deleted items or outdated versions
			}
			if !yield(rec, nil) {
				return
			}
		}
	}
}

type Store struct {
	Dir       string
	Schemas   map[string]Schema
	Resources map[string]DB
}

func NewStore(dir string) (*Store, error) {
	s := &Store{Dir: dir, Schemas: map[string]Schema{}, Resources: map[string]DB{}}
	schemaDB, err := NewCSVDB(s.Dir + "/_schemas.csv")
	if err != nil {
		return nil, err
	}
	for rec, err := range schemaDB.Iter() {
		if err != nil {
			return nil, err
		}
		if len(rec) != 8 {
			return nil, fmt.Errorf("invalid schema record: %v", rec)
		}
		schema := FieldSchema{
			Resource: rec[2],
			Field:    rec[3],
			Type:     FieldType(rec[4]),
			Regex:    rec[7],
		}
		schema.Min, _ = strconv.ParseFloat(rec[5], 64)
		schema.Max, _ = strconv.ParseFloat(rec[6], 64)
		s.Schemas[schema.Resource] = append(s.Schemas[schema.Resource], schema)
		if _, ok := s.Resources[schema.Resource]; !ok {
			db, err := NewCSVDB(s.Dir + "/" + schema.Resource + ".csv")
			if err != nil {
				return nil, err
			}
			s.Resources[schema.Resource] = db
		}
	}
	return s, nil
}

func (s *Store) Create(resource string, r Resource) (string, error) {
	db, ok := s.Resources[resource]
	if !ok {
		return "", fmt.Errorf("resource %s not found", resource)
	}
	newID := ID()
	r["_id"] = newID
	r["_v"] = 1.0
	rec, err := s.Schemas[resource].Record(r)
	if err != nil {
		return "", err
	}
	return newID, db.Create(rec)
}

func (s *Store) Update(resource string, r Resource) error {
	db, ok := s.Resources[resource]
	if !ok {
		return fmt.Errorf("resource %s not found", resource)
	}
	orig, err := s.Get(resource, r["_id"].(string))
	if err != nil {
		return fmt.Errorf("record not found: %w", err)
	}
	for _, field := range s.Schemas[resource] {
		if _, ok := r[field.Field]; !ok {
			r[field.Field] = orig[field.Field]
		}
	}
	r["_v"] = orig["_v"].(float64) + 1
	rec, err := s.Schemas[resource].Record(r)
	if err != nil {
		return err
	}
	return db.Update(rec)
}

func (s *Store) Delete(resource, id string) error {
	db, ok := s.Resources[resource]
	if !ok {
		return fmt.Errorf("resource %s not found", resource)
	}
	return db.Delete(id)
}

func (s *Store) Get(resource, id string) (Resource, error) {
	db, ok := s.Resources[resource]
	if !ok {
		return nil, fmt.Errorf("resource %s not found", resource)
	}
	rec, err := db.Get(id)
	if err != nil {
		return nil, err
	}
	if len(rec) < 2 {
		return nil, nil // record not found
	}
	return s.Schemas[resource].Resource(rec)
}

func (s *Store) List(resource, sortBy string) ([]Resource, error) {
	db, ok := s.Resources[resource]
	if !ok {
		return nil, fmt.Errorf("resource %s not found", resource)
	}
	res := []Resource{}
	for rec, err := range db.Iter() {
		if err != nil {
			return nil, err
		}
		if len(rec) < 2 {
			continue
		}
		r, err := s.Schemas[resource].Resource(rec)
		if err != nil {
			return res, err
		}
		res = append(res, r)
	}
	if sortBy != "" {
		sort.Slice(res, func(i, j int) bool {
			if res[i][sortBy] == nil {
				return false
			}
			if res[j][sortBy] == nil {
				return true
			}
			switch res[i][sortBy].(type) {
			case string:
				return res[i][sortBy].(string) < res[j][sortBy].(string)
			case float64:
				return res[i][sortBy].(float64) < res[j][sortBy].(float64)
			default:
				return false
			}
		})
	}
	return res, nil
}

func main() {}
