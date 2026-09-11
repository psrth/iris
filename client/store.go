package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store keeps connected sessions on disk, one file per uid under Dir.
type Store struct {
	Dir string
}

// MaxAge is how long a stored session is kept: a session's inactivity TTL
// plus its grace period, after which the relay has purged it anyway.
const MaxAge = 48 * time.Hour

// Save records a session under its uid.
func (st Store) Save(uid string, s Session) error {
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(st.Dir, uid), b, 0o600)
}

// Remove forgets a session.
func (st Store) Remove(uid string) error {
	err := os.Remove(filepath.Join(st.Dir, uid))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// List returns the stored uids, oldest first, dropping entries past MaxAge.
func (st Store) List() ([]string, error) {
	ents, err := os.ReadDir(st.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type ent struct {
		uid string
		at  time.Time
	}
	var live []ent
	for _, e := range ents {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		if time.Since(info.ModTime()) > MaxAge {
			os.Remove(filepath.Join(st.Dir, e.Name()))
			continue
		}
		live = append(live, ent{e.Name(), info.ModTime()})
	}
	sort.Slice(live, func(i, j int) bool { return live[i].at.Before(live[j].at) })
	uids := make([]string, len(live))
	for i, e := range live {
		uids[i] = e.uid
	}
	return uids, nil
}

// Load returns the session for uid. With an empty uid it returns the only
// stored session, or an error naming the candidates when there are several.
func (st Store) Load(uid string) (Session, error) {
	if uid == "" {
		uids, err := st.List()
		if err != nil {
			return Session{}, err
		}
		switch len(uids) {
		case 0:
			return Session{}, errors.New("no session; run iris connect <token> first")
		case 1:
			uid = uids[0]
		default:
			return Session{}, fmt.Errorf("several sessions; pick one with -s: %s", strings.Join(uids, " "))
		}
	}
	b, err := os.ReadFile(filepath.Join(st.Dir, uid))
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, fmt.Errorf("no session %s; run iris connect <token> first", uid)
	}
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return Session{}, fmt.Errorf("session %s: %w", uid, err)
	}
	return s, nil
}
