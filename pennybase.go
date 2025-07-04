package pennybase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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
var SessionKey = Salt()

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

func SignSession(username string) string {
	data := fmt.Sprintf("%s:%d", username, time.Now().Unix())
	sum := sha256.Sum256([]byte(SessionKey + data))
	sig := base32.StdEncoding.EncodeToString(sum[:])[:16]
	return fmt.Sprintf("%s.%s", data, sig)
}

func VerifySession(session string) (string, bool) {
	parts := strings.Split(session, ".")
	if len(parts) != 2 {
		return "", false
	}
	data, sig := parts[0], parts[1]
	sum := sha256.Sum256([]byte(SessionKey + data))
	expectedSig := base32.StdEncoding.EncodeToString(sum[:])[:16]
	if sig != expectedSig {
		return "", false
	}
	if parts = strings.Split(data, ":"); len(parts) == 2 {
		if ts, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			if time.Now().Unix()-ts < 86400 { // 24 hours
				return parts[0], true
			}
		}
	}
	return "", false
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
	if err := db.Create(rec); err != nil {
		return "", err
	}
	return newID, nil
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

func (s *Store) Close() error {
	for _, db := range s.Resources {
		if err := db.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Authenticate(r *http.Request) (Resource, error) {
	if cookie, err := r.Cookie("session"); err == nil {
		if username, ok := VerifySession(cookie.Value); ok {
			u, err := s.Get("_users", username)
			if err != nil {
				return nil, fmt.Errorf("users error: %w", err)
			}
			return u, nil
		}
	}
	if username, password, ok := r.BasicAuth(); ok {
		return s.AuthenticateBasic(username, password)
	}
	return nil, errors.New("unauthenticated")
}

func (s *Store) AuthenticateBasic(username, password string) (Resource, error) {
	u, err := s.Get("_users", username)
	if err != nil {
		return nil, fmt.Errorf("users error: %w", err)
	}
	if u["password"] != HashPasswd(password, u["salt"].(string)) {
		return nil, errors.New("unauthenticated")
	}
	return u, nil
}

func (s *Store) Authorize(resource, id, action string, user Resource) error {
	permissions, err := s.List("_permissions", "")
	if err != nil {
		return fmt.Errorf("permissions error: %w", err)
	}
	for _, p := range permissions {
		if p["resource"] != resource || (p["action"] != "*" && p["action"] != action) {
			continue
		}
		if p["field"] == "" && p["role"] == "" { // public
			return nil
		}
		if user == nil {
			return errors.New("unauthenticated")
		}
		// Any role? Or user has the role?
		if p["role"] == "*" || slices.Contains(user["roles"].([]string), p["role"].(string)) {
			return nil
		}
		if id != "" {
			res, err := s.Get(resource, id)
			if err != nil {
				return err
			}
			username := user["_id"].(string)
			if user, ok := res[p["field"].(string)]; ok && user == username {
				return nil // user name matches requested resource field (string)
			} else if users, ok := res[p["field"].(string)].([]string); ok && slices.Contains(users, username) {
				return nil // user name is in the requested resource field (list)
			}
		}
	}
	return errors.New("unauthorized")
}

type Event struct {
	Action string   `json:"action"`
	ID     string   `json:"id"`
	Data   Resource `json:"data"`
}

type Broker struct {
	channels map[string]map[chan Event]bool // resource -> channels
	mu       sync.RWMutex
}

func (b *Broker) Subscribe(resource string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.channels[resource] == nil {
		b.channels[resource] = make(map[chan Event]bool)
	}
	b.channels[resource][ch] = true
}

func (b *Broker) Unsubscribe(resource string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if subs := b.channels[resource]; subs != nil {
		delete(subs, ch)
	}
}

func (b *Broker) Publish(resource string, evt Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if subs := b.channels[resource]; subs != nil {
		for ch := range subs {
			select {
			case ch <- evt:
			default:
			}
		}
	}
}

type Hook func(trigger, resource string, user, r Resource) error

func nopHook(trigger, resource string, user, r Resource) error { return nil }

type Server struct {
	Store  *Store
	Broker *Broker
	Mux    *http.ServeMux
	Hook   Hook
}

func NewServer(dataDir, tmplDir, staticDir string) (*Server, error) {
	store, err := NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{Store: store, Broker: &Broker{channels: map[string]map[chan Event]bool{}}, Mux: http.NewServeMux(), Hook: nopHook}
	auth := func(next http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resource := r.PathValue("resource")
			action := map[string]string{"GET": "read", "POST": "create", "PUT": "update", "DELETE": "delete"}[r.Method]
			user, _ := s.Store.Authenticate(r)
			if resource != "" && action != "" {
				if err := s.Store.Authorize(resource, r.PathValue("id"), action, user); err != nil {
					http.Error(w, err.Error(), http.StatusUnauthorized)
					return
				}
			}
			next(w, r.WithContext(context.WithValue(r.Context(), "user", user)))
		})
	}
	s.Mux.Handle("GET /api/{resource}/", auth(s.handleList))
	s.Mux.Handle("POST /api/{resource}/", auth(s.handleCreate))
	s.Mux.Handle("GET /api/{resource}/{id}", auth(s.handleGet))
	s.Mux.Handle("PUT /api/{resource}/{id}", auth(s.handleUpdate))
	s.Mux.Handle("DELETE /api/{resource}/{id}", auth(s.handleDelete))
	s.Mux.HandleFunc("GET /api/events/{resource}", s.handleEvents)
	s.Mux.HandleFunc("POST /api/login", s.handleLogin)
	s.Mux.HandleFunc("POST /api/logout", s.handleLogout)
	if tmplDir != "" {
		if tmpl, err := template.ParseGlob(filepath.Join(tmplDir, "*")); err == nil {
			for _, t := range tmpl.Templates() {
				if t.Name() == "index.html" {
					s.Mux.Handle("GET /", s.handleTemplate(t, "index.html"))
				}
				s.Mux.Handle(fmt.Sprintf("GET /%s", t.Name()), s.handleTemplate(tmpl, t.Name()))
			}
		} else {
			log.Fatal("Error parsing templates:", err)
		}
	}
	if staticDir != "" {
		s.Mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	}

	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.Mux.ServeHTTP(w, r) }

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	res, err := s.Store.List(r.PathValue("resource"), r.FormValue("sort_by"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var res Resource
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resource := r.PathValue("resource")
	if err := s.Hook("create", resource, r.Context().Value("user").(Resource), res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, err := s.Store.Create(resource, res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.Broker.Publish(resource, Event{Action: "created", ID: res["_id"].(string), Data: res})
	w.Header().Set("Location", fmt.Sprintf("/api/%s/%s", resource, id))
	w.Header().Set("HX-Trigger", fmt.Sprintf("%s-changed", resource))
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	res, err := s.Store.Get(r.PathValue("resource"), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if res == nil {
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	res := Resource{}
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resource := r.PathValue("resource")
	res["_id"] = r.PathValue("id")
	if err := s.Hook("update", resource, r.Context().Value("user").(Resource), res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.Store.Update(resource, res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.Broker.Publish(resource, Event{Action: "updated", ID: res["_id"].(string), Data: res})
	w.Header().Set("HX-Trigger", fmt.Sprintf("%s-changed", resource))
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	res, _ := s.Store.Get(r.PathValue("resource"), r.PathValue("id"))
	if err := s.Hook("delete", r.PathValue("resource"), r.Context().Value("user").(Resource), res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.Store.Delete(r.PathValue("resource"), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.Broker.Publish(r.PathValue("resource"), Event{Action: "deleted", Data: res})
	w.Header().Set("HX-Trigger", fmt.Sprintf("%s-changed", r.PathValue("resource")))
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	username, password := r.FormValue("username"), r.FormValue("password")
	if _, err := s.Store.AuthenticateBasic(username, password); err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    SignSession(username),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24 hours
	})
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTemplate(tmpl *template.Template, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := s.Store.Authenticate(r)
		data := map[string]any{
			"Store":   s.Store,
			"Request": r,
			"User":    user,
			"ID":      r.URL.Query().Get("_id"),
			"Authorize": func(resource, id, action string) bool {
				return s.Store.Authorize(resource, id, action, user) == nil
			},
		}
		if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
			log.Println("Error executing template:", name, err)
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	resource := r.PathValue("resource")
	user, err := s.Store.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	events := make(chan Event, 10)
	s.Broker.Subscribe(resource, events)
	defer s.Broker.Unsubscribe(resource, events)
	for {
		select {
		case e := <-events:
			if e.Action == "delete" || s.Store.Authorize(resource, e.ID, "read", user) == nil {
				data, _ := json.Marshal(e.Data)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Action, data)
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}
