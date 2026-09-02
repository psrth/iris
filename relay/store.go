package relay

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	uid        TEXT PRIMARY KEY,
	key_hash   BLOB NOT NULL,
	state      INTEGER NOT NULL DEFAULT 0,
	created    INTEGER NOT NULL,
	last_write INTEGER NOT NULL,
	ro_at      INTEGER NOT NULL DEFAULT 0,
	head       INTEGER NOT NULL DEFAULT 0,
	storage    INTEGER NOT NULL DEFAULT 0,
	warned     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	uid      TEXT NOT NULL,
	seq      INTEGER NOT NULL,
	ts       INTEGER NOT NULL,
	urgent   INTEGER NOT NULL DEFAULT 0,
	message  BLOB NOT NULL,
	metadata BLOB NOT NULL,
	PRIMARY KEY (uid, seq)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS files (
	uid          TEXT NOT NULL,
	name         TEXT NOT NULL,
	size         INTEGER NOT NULL,
	content_type TEXT NOT NULL,
	seq          INTEGER NOT NULL,
	uploaded_at  INTEGER NOT NULL,
	PRIMARY KEY (uid, name)
) WITHOUT ROWID;
`

// Session states.
const (
	stateActive   = 0
	stateReadOnly = 1
	statePurged   = 2
)

// One-shot warning flags, stored as bits in sessions.warned.
const (
	warnExpiry = 1 << iota // reset by every write
	warnMessages
	warnStorage
)

// errRefused: the session is not active or is at its message cap.
var errRefused = errors.New("session refuses writes")

type session struct {
	uid       string
	keyHash   []byte
	state     int
	created   int64
	lastWrite int64
	roAt      int64
	head      int64
	storage   int64
	warned    int
}

// Envelope is one entry in a session's log.
type Envelope struct {
	Seq      int64           `json:"seq"`
	TS       string          `json:"ts"`
	Message  json.RawMessage `json:"message"`
	Metadata json.RawMessage `json:"metadata"`
}

type fileInfo struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Seq         int64  `json:"seq"`
	UploadedAt  string `json:"uploaded_at"`
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer, no SQLITE_BUSY, trivial serialization
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func stamp(unix int64) string {
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func (r *Relay) getSession(uid string) (*session, error) {
	s := &session{uid: uid}
	err := r.db.QueryRow(`SELECT key_hash, state, created, last_write, ro_at, head, storage, warned
		FROM sessions WHERE uid = ?`, uid).
		Scan(&s.keyHash, &s.state, &s.created, &s.lastWrite, &s.roAt, &s.head, &s.storage, &s.warned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// appendTx stamps and inserts one message. touch marks write activity;
// cap, when non-zero, refuses the append once head reaches it. Only active
// sessions accept appends.
func appendTx(tx *sql.Tx, uid string, msg, meta json.RawMessage, urgent, touch bool, cap int64, now int64) (Envelope, error) {
	var seq int64
	err := tx.QueryRow(`UPDATE sessions SET
			head = head + 1,
			last_write = CASE WHEN ?1 THEN ?2 ELSE last_write END,
			warned = CASE WHEN ?1 THEN warned & ~?3 ELSE warned END
		WHERE uid = ?4 AND state = ?5 AND (?6 = 0 OR head < ?6)
		RETURNING head`,
		touch, now, warnExpiry, uid, stateActive, cap).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return Envelope{}, errRefused
	}
	if err != nil {
		return Envelope{}, err
	}
	_, err = tx.Exec(`INSERT INTO messages (uid, seq, ts, urgent, message, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
		uid, seq, now, urgent, []byte(msg), []byte(meta))
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Seq: seq, TS: stamp(now), Message: msg, Metadata: meta}, nil
}

// append runs appendTx in its own transaction and wakes waiters.
func (r *Relay) append(uid string, msg, meta json.RawMessage, urgent, touch bool, cap int64) (Envelope, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return Envelope{}, err
	}
	defer tx.Rollback()
	env, err := appendTx(tx, uid, msg, meta, urgent, touch, cap, r.now().Unix())
	if err != nil {
		return Envelope{}, err
	}
	if err := tx.Commit(); err != nil {
		return Envelope{}, err
	}
	r.hub.pub(uid)
	return env, nil
}

// readLog returns up to limit messages with seq > since, and the session head.
func (r *Relay) readLog(uid string, since, limit int64, urgentOnly bool) ([]Envelope, int64, error) {
	q := `SELECT seq, ts, message, metadata FROM messages WHERE uid = ? AND seq > ?`
	if urgentOnly {
		q += ` AND urgent = 1`
	}
	rows, err := r.db.Query(q+` ORDER BY seq LIMIT ?`, uid, since, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	msgs := []Envelope{}
	for rows.Next() {
		var e Envelope
		var ts int64
		var msg, meta []byte
		if err := rows.Scan(&e.Seq, &ts, &msg, &meta); err != nil {
			return nil, 0, err
		}
		e.TS, e.Message, e.Metadata = stamp(ts), msg, meta
		msgs = append(msgs, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var head int64
	err = r.db.QueryRow(`SELECT head FROM sessions WHERE uid = ?`, uid).Scan(&head)
	return msgs, head, err
}

func (r *Relay) listFiles(uid string) ([]fileInfo, error) {
	rows, err := r.db.Query(`SELECT name, size, content_type, seq, uploaded_at FROM files WHERE uid = ? ORDER BY name`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []fileInfo{}
	for rows.Next() {
		var f fileInfo
		var at int64
		if err := rows.Scan(&f.Name, &f.Size, &f.ContentType, &f.Seq, &at); err != nil {
			return nil, err
		}
		f.UploadedAt = stamp(at)
		files = append(files, f)
	}
	return files, rows.Err()
}

// warnOnce sets flag on the session and, if it was not already set,
// appends a system message. A refused append (session no longer active)
// is not an error; real errors are logged, since no caller can do better.
func (r *Relay) warnOnce(uid string, flag int, content string, meta any) {
	res, err := r.db.Exec(`UPDATE sessions SET warned = warned | ?1 WHERE uid = ?2 AND warned & ?1 = 0`, flag, uid)
	if err == nil {
		if n, _ := res.RowsAffected(); n == 0 {
			return
		}
		_, err = r.appendSystem(uid, content, meta)
	}
	if err != nil && !errors.Is(err, errRefused) {
		r.log.Printf("warn %s: %v", uid, err)
	}
}

// purge deletes a session's history and files; the row stays as a tombstone.
func (r *Relay) purge(uid string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM messages WHERE uid = ?`,
		`DELETE FROM files WHERE uid = ?`,
		`UPDATE sessions SET state = 2, storage = 0 WHERE uid = ?`,
	} {
		if _, err := tx.Exec(q, uid); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.hub.pub(uid)
	return os.RemoveAll(filepath.Join(r.files, uid))
}
