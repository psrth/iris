package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/psrth/iris/relay"
)

func newSession(t *testing.T) Session {
	t.Helper()
	r, err := relay.Open(relay.Config{DataDir: t.TempDir(), Limits: relay.DefaultLimits})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	srv := httptest.NewServer(r.Handler())
	t.Cleanup(srv.Close)
	uid, key, err := r.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	return Session{URL: srv.URL + "/s/" + uid, Key: key}
}

func decode(t *testing.T, r Result, v any) {
	t.Helper()
	if !r.OK() {
		t.Fatalf("status %d: %s", r.Status, r.Body)
	}
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatal(err)
	}
}

func TestPostReadWait(t *testing.T) {
	s := newSession(t)
	ctx := context.Background()

	body, err := Post{Name: "a-b-c", Text: "hello", ReplyTo: 0}.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Seq      int             `json:"seq"`
		Metadata json.RawMessage `json:"metadata"`
	}
	r, err := s.Post(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	decode(t, r, &env)
	if env.Seq != 1 || string(env.Metadata) != "{}" {
		t.Fatalf("envelope %+v", env)
	}

	var log struct {
		Messages []json.RawMessage `json:"messages"`
		LastSeq  int               `json:"last_seq"`
	}
	r, _ = s.Read(ctx, 0, 0)
	decode(t, r, &log)
	if len(log.Messages) != 1 || log.LastSeq != 1 {
		t.Fatalf("read %+v", log)
	}

	// wait wakes on the next post, and the urgent filter skips plain ones
	go func() {
		time.Sleep(100 * time.Millisecond)
		plain, _ := Post{Name: "a-b-c", Text: "plain"}.JSON()
		s.Post(ctx, plain)
		time.Sleep(100 * time.Millisecond)
		urgent, _ := Post{Name: "a-b-c", Text: "now", Urgent: true, Human: true}.JSON()
		s.Post(ctx, urgent)
	}()
	r, err = s.Wait(ctx, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	decode(t, r, &log)
	if len(log.Messages) != 1 || log.LastSeq != 3 || !strings.Contains(string(log.Messages[0]), `"attn":"human"`) {
		t.Fatalf("urgent wait %+v", log)
	}

	tctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := s.Wait(tctx, 3, false); err != ErrTimeout {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}

func TestFilesAndTerminate(t *testing.T) {
	s := newSession(t)
	ctx := context.Background()

	r, err := s.Put(ctx, "trace.log", "text/plain", strings.NewReader("line\n"))
	if err != nil {
		t.Fatal(err)
	}
	var ann struct {
		Seq      int `json:"seq"`
		Metadata struct {
			Event string `json:"event"`
		} `json:"metadata"`
	}
	decode(t, r, &ann)
	if ann.Metadata.Event != "file_uploaded" || ann.Seq != 1 {
		t.Fatalf("announcement %+v", ann)
	}

	body, _ := Post{Name: "a-b-c", Text: "see file", Files: []FilePart{{"trace.log", ann.Seq}}}.JSON()
	if !strings.Contains(string(body), `"file":{"name":"trace.log","seq":1}`) {
		t.Fatalf("file part missing: %s", body)
	}
	if r, _ = s.Post(ctx, body); !r.OK() {
		t.Fatalf("post with file part: %s", r.Body)
	}

	var buf bytes.Buffer
	if r, err = s.Get(ctx, "trace.log", &buf); err != nil || buf.String() != "line\n" {
		t.Fatalf("get: %v %q", err, buf.String())
	}
	if r, _ = s.Get(ctx, "missing", &buf); r.Status != 404 || !strings.Contains(r.Error(), "not_found") {
		t.Fatalf("missing file: %d %s", r.Status, r.Error())
	}

	var list struct {
		Files []struct{ Name string } `json:"files"`
	}
	r, _ = s.Files(ctx)
	decode(t, r, &list)
	if len(list.Files) != 1 || list.Files[0].Name != "trace.log" {
		t.Fatalf("files %+v", list)
	}

	r, _ = s.Terminate(ctx)
	if !r.OK() || !strings.Contains(string(r.Body), `"read-only"`) {
		t.Fatalf("terminate: %s", r.Body)
	}
	if r, _ = s.Post(ctx, body); r.Status != 409 {
		t.Fatalf("post after terminate: %d", r.Status)
	}
}

func TestPostJSON(t *testing.T) {
	if _, err := (Post{Text: "x"}).JSON(); err == nil {
		t.Fatal("name required")
	}
	if _, err := (Post{Name: "n"}).JSON(); err == nil {
		t.Fatal("content required")
	}
	b, _ := Post{Name: "n", Text: "t", Urgent: true, ReplyTo: 7, Extra: map[string]any{"urgent": false, "k": "v"}}.JSON()
	var v struct {
		Metadata map[string]any `json:"metadata"`
	}
	json.Unmarshal(b, &v)
	if v.Metadata["urgent"] != true || v.Metadata["reply_to"] != float64(7) || v.Metadata["k"] != "v" {
		t.Fatalf("metadata %v", v.Metadata)
	}
}

func TestStore(t *testing.T) {
	st := Store{Dir: t.TempDir() + "/sessions"}
	if _, err := st.Load(""); err == nil {
		t.Fatal("empty store must error")
	}
	st.Save("aaaa", Session{URL: "u1", Key: "k1"})
	s, err := st.Load("")
	if err != nil || s.URL != "u1" {
		t.Fatalf("single: %v %+v", err, s)
	}
	st.Save("bbbb", Session{URL: "u2", Key: "k2"})
	if _, err := st.Load(""); err == nil || !strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("ambiguous: %v", err)
	}
	if s, _ = st.Load("bbbb"); s.Key != "k2" {
		t.Fatalf("by uid: %+v", s)
	}
	st.Remove("aaaa")
	if uids, _ := st.List(); len(uids) != 1 || uids[0] != "bbbb" {
		t.Fatalf("list %v", uids)
	}
}
