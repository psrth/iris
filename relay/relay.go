// Package relay implements the iris session relay: an append-only message
// log plus a file drop per session, served over plain HTTP.
package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config configures a Relay. Zero values take defaults.
type Config struct {
	DataDir string           // SQLite log and file store live here
	Limits  Limits           // zero value means DefaultLimits
	Now     func() time.Time // clock; nil means time.Now
	Log     *log.Logger      // nil means log.Default
}

// Relay is one relay instance. Create with Open; serve via Handler.
type Relay struct {
	db     *sql.DB
	files  string
	limits Limits
	now    func() time.Time
	log    *log.Logger
	hub    hub
	msgs   *limiter // messages per session
	ips    *limiter // sessions per IP
}

// Open opens (or creates) the relay's data directory.
func Open(cfg Config) (*Relay, error) {
	if cfg.Limits == (Limits{}) {
		cfg.Limits = DefaultLimits
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = log.Default()
	}
	files := filepath.Join(cfg.DataDir, "files")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return nil, err
	}
	db, err := openDB(filepath.Join(cfg.DataDir, "iris.db"))
	if err != nil {
		return nil, err
	}
	return &Relay{
		db:     db,
		files:  files,
		limits: cfg.Limits,
		now:    cfg.Now,
		log:    cfg.Log,
		hub:    hub{m: make(map[string]chan struct{})},
		msgs:   newLimiter(time.Minute, cfg.Limits.MessagesPerMinute),
		ips:    newLimiter(time.Hour, cfg.Limits.SessionsPerIPPerHour),
	}, nil
}

// Close releases the database.
func (r *Relay) Close() error { return r.db.Close() }

// NewSession provisions a session and returns its uid and bearer key.
func (r *Relay) NewSession() (uid, key string, err error) {
	s, key, err := r.newSession()
	if err != nil {
		return "", "", err
	}
	return s.uid, key, nil
}

func (r *Relay) newSession() (*session, string, error) {
	var raw [40]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, "", err
	}
	key := base64.RawURLEncoding.EncodeToString(raw[8:])
	now := r.now().Unix()
	s := &session{uid: hex.EncodeToString(raw[:8]), keyHash: hashKey(key), created: now, lastWrite: now}
	_, err := r.db.Exec(`INSERT INTO sessions (uid, key_hash, created, last_write) VALUES (?, ?, ?, ?)`,
		s.uid, s.keyHash, s.created, s.lastWrite)
	if err != nil {
		return nil, "", err
	}
	return s, key, nil
}

func hashKey(key string) []byte {
	h := sha256.Sum256([]byte(key))
	return h[:]
}

// Run sweeps the lifecycle every 30s until ctx is done.
func (r *Relay) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Sweep()
		}
	}
}

// Sweep runs one lifecycle pass: warn sessions nearing inactivity, move
// inactive ones to read-only, purge those past grace.
func (r *Relay) Sweep() {
	now := r.now()
	ttl, grace := r.limits.InactivityTTLSeconds, r.limits.GraceSeconds
	unix := now.Unix()

	for _, uid := range r.uids(`state = ? AND warned & ? = 0 AND last_write + ? <= ?`, stateActive, warnExpiry, ttl-600, unix) {
		r.warnOnce(uid, warnExpiry, "session goes read-only in ~10 minutes without a write", map[string]any{
			"event": "session_expiring",
		})
	}
	if _, err := r.db.Exec(`UPDATE sessions SET state = ?, ro_at = ? WHERE state = ? AND last_write + ? <= ?`,
		stateReadOnly, unix, stateActive, ttl, unix); err != nil {
		r.log.Printf("sweep: deactivate: %v", err)
	}
	for _, uid := range r.uids(`state = ? AND ro_at + ? <= ?`, stateReadOnly, grace, unix) {
		if err := r.purge(uid); err != nil {
			r.log.Printf("sweep: purge %s: %v", uid, err)
		}
	}
	r.msgs.prune(now)
	r.ips.prune(now)
}

func (r *Relay) uids(where string, args ...any) []string {
	rows, err := r.db.Query(`SELECT uid FROM sessions WHERE `+where, args...)
	if err != nil {
		r.log.Printf("sweep: %v", err)
		return nil
	}
	defer rows.Close()
	var uids []string
	for rows.Next() {
		var uid string
		if rows.Scan(&uid) == nil {
			uids = append(uids, uid)
		}
	}
	return uids
}

// Handler returns the relay's HTTP surface.
func (r *Relay) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions", r.createSession)
	mux.HandleFunc("POST /s/{uid}", r.postMessage)
	mux.HandleFunc("GET /s/{uid}", r.getMessages)
	mux.HandleFunc("GET /s/{uid}/wait", r.wait)
	mux.HandleFunc("PUT /s/{uid}/files/{name}", r.putFile)
	mux.HandleFunc("GET /s/{uid}/files/{name}", r.getFile)
	mux.HandleFunc("GET /s/{uid}/files", r.listFilesHandler)
	mux.HandleFunc("POST /s/{uid}/terminate", r.terminate)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	return cors(mux)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		h.Set("Access-Control-Expose-Headers", "Retry-After")
		if req.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, req)
	})
}

// --- responses ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type errBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, struct {
		Error errBody `json:"error"`
	}{errBody{code, msg}})
}

func (r *Relay) internal(w http.ResponseWriter, err error) {
	r.log.Printf("error: %v", err)
	fail(w, http.StatusInternalServerError, "internal", "internal error")
}

func retryAfter(w http.ResponseWriter, d time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(d.Seconds())+1))
	fail(w, http.StatusTooManyRequests, "rate_limited", "rate limited")
}

// --- sessions ---

func (r *Relay) createSession(w http.ResponseWriter, req *http.Request) {
	ip, _, _ := net.SplitHostPort(req.RemoteAddr)
	if ok, wait := r.ips.allow(ip, r.now()); !ok {
		retryAfter(w, wait)
		return
	}
	s, key, err := r.newSession()
	if err != nil {
		r.internal(w, err)
		return
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	writeJSON(w, http.StatusCreated, struct {
		UID       string `json:"uid"`
		URL       string `json:"url"`
		Key       string `json:"key"`
		CreatedAt string `json:"created_at"`
		Limits    Limits `json:"limits"`
	}{s.uid, scheme + "://" + req.Host + "/s/" + s.uid, key, stamp(s.created), r.limits})
}

// auth loads the session named in the path and checks the bearer key.
// On failure it has already written the response and returns nil.
func (r *Relay) auth(w http.ResponseWriter, req *http.Request) *session {
	s, err := r.getSession(req.PathValue("uid"))
	if err != nil {
		r.internal(w, err)
		return nil
	}
	if s == nil {
		fail(w, http.StatusNotFound, "not_found", "no such session")
		return nil
	}
	if s.state == statePurged {
		fail(w, http.StatusGone, "gone", "session purged")
		return nil
	}
	key, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare(hashKey(key), s.keyHash) != 1 {
		fail(w, http.StatusUnauthorized, "unauthorized", "bad or missing bearer key")
		return nil
	}
	return s
}

// writable is auth plus the checks every write shares. The refusal for a
// message cap is left to the append itself, which is exact.
func (r *Relay) writable(w http.ResponseWriter, req *http.Request) *session {
	s := r.auth(w, req)
	if s == nil {
		return nil
	}
	if s.state != stateActive {
		fail(w, http.StatusConflict, "session_read_only", "session is read-only")
		return nil
	}
	if ok, wait := r.msgs.allow(s.uid, r.now()); !ok {
		retryAfter(w, wait)
		return nil
	}
	return s
}

// refused explains an errRefused after re-reading the session.
func (r *Relay) refused(w http.ResponseWriter, uid string) {
	s, err := r.getSession(uid)
	if err == nil && s != nil && s.state == stateActive {
		fail(w, http.StatusConflict, "limit_exceeded", "session message cap reached")
		return
	}
	fail(w, http.StatusConflict, "session_read_only", "session is read-only")
}

// --- messages ---

var handleRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func (r *Relay) postMessage(w http.ResponseWriter, req *http.Request) {
	s := r.writable(w, req)
	if s == nil {
		return
	}
	var in struct {
		Message  json.RawMessage `json:"message"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, r.limits.MaxBodyBytes)).Decode(&in); err != nil {
		var big *http.MaxBytesError
		if errors.As(err, &big) {
			fail(w, http.StatusRequestEntityTooLarge, "payload_too_large", "body exceeds max_body_bytes")
			return
		}
		fail(w, http.StatusBadRequest, "invalid_request", "body must be a JSON object")
		return
	}
	msg, err := validateMessage(in.Message)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	meta, urgent, err := validateMetadata(in.Metadata)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	env, err := r.append(s.uid, msg, meta, urgent, true, r.limits.MaxMessages)
	if errors.Is(err, errRefused) {
		r.refused(w, s.uid)
		return
	}
	if err != nil {
		r.internal(w, err)
		return
	}
	if env.Seq >= r.limits.MaxMessages*9/10 {
		r.warnOnce(s.uid, warnMessages, "session is at 90% of its message cap", map[string]any{
			"event": "limit_warning", "limit": "messages", "used": env.Seq, "max": r.limits.MaxMessages,
		})
	}
	writeJSON(w, http.StatusCreated, env)
}

// validateMessage checks the client-supplied message and returns it
// compacted but otherwise verbatim.
func validateMessage(raw json.RawMessage) (json.RawMessage, error) {
	var m struct {
		Role    string          `json:"role"`
		Name    string          `json:"name"`
		Content json.RawMessage `json:"content"`
	}
	if len(raw) == 0 {
		return nil, errors.New("message is required")
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, errors.New("message must be an object")
	}
	switch m.Role {
	case "assistant", "user":
	case "system":
		return nil, errors.New("role system is reserved for the relay")
	default:
		return nil, errors.New("message.role must be assistant or user")
	}
	if !handleRE.MatchString(m.Name) {
		return nil, errors.New("message.name must match [A-Za-z0-9._-]{1,64}")
	}
	if err := validateContent(m.Content); err != nil {
		return nil, err
	}
	return compact(raw), nil
}

func validateContent(raw json.RawMessage) error {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "" {
			return errors.New("message.content must be non-empty")
		}
		return nil
	}
	var parts []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return errors.New("message.content must be a non-empty string or array of parts")
	}
	for _, p := range parts {
		if p.Type == "" {
			return errors.New("every content part needs a type")
		}
	}
	return nil
}

// validateMetadata returns the metadata object (or {}) and its urgent flag.
func validateMetadata(raw json.RawMessage) (json.RawMessage, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}"), false, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false, errors.New("metadata must be an object")
	}
	return compact(raw), string(m["urgent"]) == "true", nil
}

func compact(raw json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return raw
	}
	return b.Bytes()
}

func (r *Relay) appendSystem(uid, content string, meta any) (Envelope, error) {
	msg, _ := json.Marshal(struct {
		Role    string `json:"role"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}{"system", "iris", content})
	md, err := json.Marshal(meta)
	if err != nil {
		return Envelope{}, err
	}
	return r.append(uid, msg, md, false, false, 0)
}

func (r *Relay) getMessages(w http.ResponseWriter, req *http.Request) {
	s := r.auth(w, req)
	if s == nil {
		return
	}
	since, limit, ok := r.window(w, req, 200)
	if !ok {
		return
	}
	msgs, head, err := r.readLog(s.uid, since, limit, false)
	if err != nil {
		r.internal(w, err)
		return
	}
	writeLog(w, msgs, head)
}

func (r *Relay) wait(w http.ResponseWriter, req *http.Request) {
	s := r.auth(w, req)
	if s == nil {
		return
	}
	since, _, ok := r.window(w, req, 200)
	if !ok {
		return
	}
	q := req.URL.Query()
	timeout := r.limits.MaxWaitSeconds
	if v := q.Get("timeout"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			fail(w, http.StatusBadRequest, "invalid_request", "timeout must be a non-negative integer")
			return
		}
		timeout = min(n, r.limits.MaxWaitSeconds)
	}
	filter := q.Get("filter")
	if filter != "" && filter != "urgent" {
		fail(w, http.StatusBadRequest, "invalid_request", "filter must be urgent")
		return
	}
	deadline := time.NewTimer(time.Duration(timeout) * time.Second)
	defer deadline.Stop()
	for {
		wake := r.hub.sub(s.uid)
		msgs, head, err := r.readLog(s.uid, since, 200, filter == "urgent")
		if err != nil {
			r.internal(w, err)
			return
		}
		if len(msgs) > 0 {
			writeLog(w, msgs, head)
			return
		}
		if timeout == 0 {
			break
		}
		select {
		case <-wake:
			continue
		case <-deadline.C:
		case <-req.Context().Done():
		}
		break
	}
	w.WriteHeader(http.StatusNoContent)
}

// window parses since and limit.
func (r *Relay) window(w http.ResponseWriter, req *http.Request, defLimit int64) (since, limit int64, ok bool) {
	q := req.URL.Query()
	since, limit = 0, defLimit
	var err error
	if v := q.Get("since"); v != "" {
		if since, err = strconv.ParseInt(v, 10, 64); err != nil || since < 0 {
			fail(w, http.StatusBadRequest, "invalid_request", "since must be a non-negative integer")
			return 0, 0, false
		}
	}
	if v := q.Get("limit"); v != "" {
		if limit, err = strconv.ParseInt(v, 10, 64); err != nil || limit < 1 {
			fail(w, http.StatusBadRequest, "invalid_request", "limit must be a positive integer")
			return 0, 0, false
		}
		limit = min(limit, 1000)
	}
	return since, limit, true
}

func writeLog(w http.ResponseWriter, msgs []Envelope, head int64) {
	writeJSON(w, http.StatusOK, struct {
		Messages []Envelope `json:"messages"`
		LastSeq  int64      `json:"last_seq"`
	}{msgs, head})
}

// --- files ---

var fileRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)

func (r *Relay) putFile(w http.ResponseWriter, req *http.Request) {
	s := r.writable(w, req)
	if s == nil {
		return
	}
	name := req.PathValue("name")
	if !fileRE.MatchString(name) || name == "." || name == ".." {
		fail(w, http.StatusBadRequest, "invalid_request", "file name must match [A-Za-z0-9._-]{1,255}")
		return
	}
	ct := req.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	dir := filepath.Join(r.files, s.uid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.internal(w, err)
		return
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		r.internal(w, err)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	size, err := io.Copy(tmp, http.MaxBytesReader(w, req.Body, r.limits.MaxFileBytes))
	if err != nil {
		var big *http.MaxBytesError
		if errors.As(err, &big) {
			fail(w, http.StatusRequestEntityTooLarge, "payload_too_large", "file exceeds max_file_bytes")
			return
		}
		fail(w, http.StatusBadRequest, "invalid_request", "could not read body")
		return
	}
	if err := tmp.Sync(); err != nil {
		r.internal(w, err)
		return
	}

	env, storage, err := r.recordFile(s.uid, name, size, ct)
	if errors.Is(err, errRefused) {
		fail(w, http.StatusConflict, "session_read_only", "session is read-only")
		return
	}
	if errors.Is(err, errStorage) {
		fail(w, http.StatusConflict, "limit_exceeded", "session storage cap reached")
		return
	}
	if err != nil {
		r.internal(w, err)
		return
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		r.internal(w, err)
		return
	}
	r.hub.pub(s.uid)
	if storage >= r.limits.MaxStorageBytes/10*9 {
		r.warnOnce(s.uid, warnStorage, "session is at 90% of its storage cap", map[string]any{
			"event": "limit_warning", "limit": "storage", "used": storage, "max": r.limits.MaxStorageBytes,
		})
	}
	writeJSON(w, http.StatusCreated, env)
}

var errStorage = errors.New("storage cap")

// recordFile accounts for an upload and appends its announcement, in one
// transaction. It returns the announcement and the session's new usage.
func (r *Relay) recordFile(uid, name string, size int64, ct string) (Envelope, int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return Envelope{}, 0, err
	}
	defer tx.Rollback()
	var old int64
	err = tx.QueryRow(`SELECT size FROM files WHERE uid = ? AND name = ?`, uid, name).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Envelope{}, 0, err
	}
	var storage int64
	err = tx.QueryRow(`UPDATE sessions SET storage = storage - ?1 + ?2
		WHERE uid = ?3 AND storage - ?1 + ?2 <= ?4 RETURNING storage`,
		old, size, uid, r.limits.MaxStorageBytes).Scan(&storage)
	if errors.Is(err, sql.ErrNoRows) {
		return Envelope{}, 0, errStorage
	}
	if err != nil {
		return Envelope{}, 0, err
	}
	now := r.now().Unix()
	msg, _ := json.Marshal(struct {
		Role    string `json:"role"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}{"system", "iris", fmt.Sprintf("uploaded %s (%d bytes, %s)", name, size, ct)})
	meta, _ := json.Marshal(map[string]any{
		"event": "file_uploaded",
		"file":  map[string]any{"name": name, "size": size, "content_type": ct},
	})
	env, err := appendTx(tx, uid, msg, meta, false, true, 0, now)
	if err != nil {
		return Envelope{}, 0, err
	}
	_, err = tx.Exec(`INSERT INTO files (uid, name, size, content_type, seq, uploaded_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (uid, name) DO UPDATE SET size = excluded.size, content_type = excluded.content_type,
		seq = excluded.seq, uploaded_at = excluded.uploaded_at`,
		uid, name, size, ct, env.Seq, now)
	if err != nil {
		return Envelope{}, 0, err
	}
	return env, storage, tx.Commit()
}

func (r *Relay) getFile(w http.ResponseWriter, req *http.Request) {
	s := r.auth(w, req)
	if s == nil {
		return
	}
	name := req.PathValue("name")
	var ct string
	var at int64
	err := r.db.QueryRow(`SELECT content_type, uploaded_at FROM files WHERE uid = ? AND name = ?`, s.uid, name).Scan(&ct, &at)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	if err != nil {
		r.internal(w, err)
		return
	}
	f, err := os.Open(filepath.Join(r.files, s.uid, name))
	if err != nil {
		r.internal(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", ct)
	http.ServeContent(w, req, name, time.Unix(at, 0), f)
}

func (r *Relay) listFilesHandler(w http.ResponseWriter, req *http.Request) {
	s := r.auth(w, req)
	if s == nil {
		return
	}
	files, err := r.listFiles(s.uid)
	if err != nil {
		r.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Files []fileInfo `json:"files"`
	}{files})
}

// --- lifecycle ---

func (r *Relay) terminate(w http.ResponseWriter, req *http.Request) {
	s := r.auth(w, req)
	if s == nil {
		return
	}
	if s.state == stateActive {
		if _, err := r.appendSystem(s.uid, "session terminated", map[string]any{"event": "session_terminated"}); err != nil && !errors.Is(err, errRefused) {
			r.internal(w, err)
			return
		}
		s.roAt = r.now().Unix()
		if _, err := r.db.Exec(`UPDATE sessions SET state = ?, ro_at = ? WHERE uid = ? AND state = ?`,
			stateReadOnly, s.roAt, s.uid, stateActive); err != nil {
			r.internal(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Status  string `json:"status"`
		PurgeAt string `json:"purge_at"`
	}{"read-only", stamp(s.roAt + r.limits.GraceSeconds)})
}
