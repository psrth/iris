package relay

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// rig is a relay under test with a controllable clock.
type rig struct {
	t     *testing.T
	r     *Relay
	srv   *httptest.Server
	clock atomic.Int64 // unix seconds
	uid   string
	key   string
}

func newRig(t *testing.T, lim Limits) *rig {
	t.Helper()
	g := &rig{t: t}
	g.clock.Store(time.Now().Unix())
	r, err := Open(Config{
		DataDir: t.TempDir(),
		Limits:  lim,
		Now:     func() time.Time { return time.Unix(g.clock.Load(), 0) },
		Log:     log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	g.r = r
	g.srv = httptest.NewServer(r.Handler())
	t.Cleanup(func() { g.srv.Close(); r.Close() })
	var out struct{ UID, Key string }
	g.do("POST", "/sessions", "", "", 201, &out)
	g.uid, g.key = out.UID, out.Key
	return g
}

func (g *rig) advance(d time.Duration) { g.clock.Add(int64(d.Seconds())) }

// do issues a request and decodes the JSON body into out (if non-nil).
func (g *rig) do(method, path, key string, body any, want int, out any) *http.Response {
	g.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		buf, _ := json.Marshal(b)
		rd = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, g.srv.URL+path, rd)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != want {
		g.t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			g.t.Fatalf("%s %s: bad JSON %q: %v", method, path, raw, err)
		}
	}
	return res
}

func (g *rig) post(content string, meta any) Envelope {
	g.t.Helper()
	var env Envelope
	g.do("POST", "/s/"+g.uid, g.key, map[string]any{
		"message":  map[string]any{"role": "assistant", "name": "a", "content": content},
		"metadata": meta,
	}, 201, &env)
	return env
}

type logPage struct {
	Messages []Envelope `json:"messages"`
	LastSeq  int64      `json:"last_seq"`
}

func (g *rig) get(path string, want int) logPage {
	g.t.Helper()
	var p logPage
	if want == 200 {
		g.do("GET", "/s/"+g.uid+path, g.key, nil, want, &p)
	} else {
		g.do("GET", "/s/"+g.uid+path, g.key, nil, want, nil)
	}
	return p
}

func event(t *testing.T, e Envelope) string {
	t.Helper()
	var m struct{ Event string }
	if err := json.Unmarshal(e.Metadata, &m); err != nil {
		t.Fatal(err)
	}
	return m.Event
}

func TestProvisionAndLog(t *testing.T) {
	g := newRig(t, DefaultLimits)
	var out struct {
		URL    string
		Limits Limits
	}
	g.do("POST", "/sessions", "", "", 201, &out)
	if out.Limits != DefaultLimits || !strings.Contains(out.URL, "/s/") {
		t.Fatalf("bad provisioning response: %+v", out)
	}

	e := g.post("hello", map[string]any{"reply_to": 0, "x": "kept"})
	if e.Seq != 1 || e.TS == "" || !strings.Contains(string(e.Metadata), `"x":"kept"`) {
		t.Fatalf("bad envelope: %+v", e)
	}
	g.post("again", nil)

	p := g.get("", 200)
	if len(p.Messages) != 2 || p.LastSeq != 2 || p.Messages[1].Seq != 2 {
		t.Fatalf("bad page: %+v", p)
	}
	if string(p.Messages[1].Metadata) != "{}" {
		t.Fatalf("absent metadata should read as {}, got %s", p.Messages[1].Metadata)
	}
	if p = g.get("?since=2", 200); len(p.Messages) != 0 || p.LastSeq != 2 {
		t.Fatalf("since=2: %+v", p)
	}
	if p = g.get("?since=0&limit=1", 200); len(p.Messages) != 1 {
		t.Fatalf("limit=1: %+v", p)
	}
	g.get("?since=-1", 400)
	g.get("?limit=0", 400)
}

func TestValidation(t *testing.T) {
	g := newRig(t, DefaultLimits)
	bad := func(msg any) {
		t.Helper()
		g.do("POST", "/s/"+g.uid, g.key, map[string]any{"message": msg}, 400, nil)
	}
	bad(map[string]any{"role": "system", "name": "iris", "content": "x"})
	bad(map[string]any{"role": "tool", "name": "a", "content": "x"})
	bad(map[string]any{"role": "user", "name": "bad handle!", "content": "x"})
	bad(map[string]any{"role": "user", "name": "a", "content": ""})
	bad(map[string]any{"role": "user", "name": "a", "content": []any{}})
	bad(map[string]any{"role": "user", "name": "a", "content": []any{map[string]any{"text": "no type"}}})
	bad("not an object")
	g.do("POST", "/s/"+g.uid, g.key, map[string]any{
		"message": map[string]any{"role": "user", "name": "a", "content": "x"}, "metadata": "str",
	}, 400, nil)
	g.do("POST", "/s/"+g.uid, g.key, "{not json", 400, nil)

	// Typed parts, including unknown types, pass through verbatim.
	var env Envelope
	g.do("POST", "/s/"+g.uid, g.key, map[string]any{"message": map[string]any{
		"role": "user", "name": "h", "content": []any{
			map[string]any{"type": "text", "text": "hi"},
			map[string]any{"type": "custom", "blob": 1},
		}}}, 201, &env)
	if !strings.Contains(string(env.Message), `"type":"custom"`) {
		t.Fatalf("parts not verbatim: %s", env.Message)
	}

	big := strings.Repeat("x", int(DefaultLimits.MaxBodyBytes))
	g.do("POST", "/s/"+g.uid, g.key, map[string]any{"message": map[string]any{"role": "user", "name": "a", "content": big}}, 413, nil)

	g.do("GET", "/s/"+g.uid, "", nil, 401, nil)
	g.do("GET", "/s/"+g.uid, "wrong", nil, 401, nil)
	g.do("GET", "/s/nope", g.key, nil, 404, nil)
	g.do("GET", "/nope", "", nil, 404, nil)
	g.do("GET", "/s/"+g.uid+"/wait?filter=all", g.key, nil, 400, nil)
	g.do("GET", "/s/"+g.uid+"/wait?timeout=x", g.key, nil, 400, nil)
}

func TestWait(t *testing.T) {
	g := newRig(t, DefaultLimits)
	g.get("/wait?timeout=0", 204)

	done := make(chan logPage)
	go func() { done <- g.get("/wait?since=0&timeout=5", 200) }()
	time.Sleep(50 * time.Millisecond)
	g.post("wake", nil)
	select {
	case p := <-done:
		if len(p.Messages) != 1 || p.Messages[0].Seq != 1 {
			t.Fatalf("bad wake: %+v", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait did not wake")
	}

	// Existing matches return immediately; timeout 0 is an instant poll.
	if p := g.get("/wait?since=0&timeout=0", 200); len(p.Messages) != 1 {
		t.Fatalf("existing match: %+v", p)
	}

	// Urgent filter ignores ordinary traffic and returns only matches.
	g.post("noise", map[string]any{"urgent": false})
	g.get("/wait?since=1&timeout=0&filter=urgent", 204)
	go func() { done <- g.get("/wait?since=1&timeout=5&filter=urgent", 200) }()
	time.Sleep(50 * time.Millisecond)
	g.post("more noise", nil)
	time.Sleep(50 * time.Millisecond)
	g.post("now!", map[string]any{"urgent": true})
	select {
	case p := <-done:
		if len(p.Messages) != 1 || p.Messages[0].Seq != 4 || p.LastSeq != 4 {
			t.Fatalf("bad urgent wake: %+v", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("urgent wait did not wake")
	}
}

func TestFiles(t *testing.T) {
	lim := DefaultLimits
	lim.MaxFileBytes = 16
	lim.MaxStorageBytes = 24
	g := newRig(t, lim)
	put := func(name, body string, want int) Envelope {
		t.Helper()
		var env Envelope
		req, _ := http.NewRequest("PUT", g.srv.URL+"/s/"+g.uid+"/files/"+name, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+g.key)
		req.Header.Set("Content-Type", "text/plain")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("PUT %s: got %d want %d: %s", name, res.StatusCode, want, raw)
		}
		if want == 201 {
			json.Unmarshal(raw, &env)
		}
		return env
	}

	env := put("trace.txt", "0123456789", 201)
	if env.Seq != 1 || event(t, env) != "file_uploaded" {
		t.Fatalf("bad announcement: %+v", env)
	}
	// Overwrite is last-write-wins with a fresh announcement.
	if env = put("trace.txt", "abc", 201); env.Seq != 2 {
		t.Fatalf("overwrite seq: %+v", env)
	}
	var list struct{ Files []fileInfo }
	g.do("GET", "/s/"+g.uid+"/files", g.key, nil, 200, &list)
	if len(list.Files) != 1 || list.Files[0].Size != 3 || list.Files[0].Seq != 2 || list.Files[0].ContentType != "text/plain" {
		t.Fatalf("bad listing: %+v", list)
	}
	res := g.do("GET", "/s/"+g.uid+"/files/trace.txt", g.key, nil, 200, nil)
	if res.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("content type: %s", res.Header.Get("Content-Type"))
	}
	g.do("GET", "/s/"+g.uid+"/files/missing", g.key, nil, 404, nil)

	put("bad%20name", "x", 400)
	put("toolarge", strings.Repeat("x", 17), 413)
	put("fills", strings.Repeat("x", 16), 201) // 3 + 16 = 19
	put("over", strings.Repeat("x", 6), 409)   // 25 > 24
	put("fills", strings.Repeat("x", 16), 201) // overwrite keeps usage at 19
	g.do("GET", "/s/"+g.uid+"/files", g.key, nil, 200, &list)
	if len(list.Files) != 2 {
		t.Fatalf("listing after cap: %+v", list)
	}
	// Storage warning fired once at 90%.
	p := g.get("", 200)
	var warns int
	for _, m := range p.Messages {
		if event(t, m) == "limit_warning" {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("storage warnings: %d", warns)
	}
}

func TestTerminate(t *testing.T) {
	g := newRig(t, DefaultLimits)
	g.post("hi", nil)
	var out struct {
		Status  string `json:"status"`
		PurgeAt string `json:"purge_at"`
	}
	g.do("POST", "/s/"+g.uid+"/terminate", g.key, nil, 200, &out)
	if out.Status != "read-only" || out.PurgeAt == "" {
		t.Fatalf("terminate: %+v", out)
	}
	p := g.get("", 200)
	if len(p.Messages) != 2 || event(t, p.Messages[1]) != "session_terminated" {
		t.Fatalf("no terminated event: %+v", p)
	}
	g.do("POST", "/s/"+g.uid, g.key, map[string]any{"message": map[string]any{"role": "user", "name": "a", "content": "x"}}, 409, nil)
	g.do("POST", "/s/"+g.uid+"/terminate", g.key, nil, 200, nil)
	if p = g.get("", 200); len(p.Messages) != 2 {
		t.Fatalf("second terminate appended: %+v", p)
	}
}

func TestLifecycle(t *testing.T) {
	g := newRig(t, DefaultLimits)
	g.post("hi", nil)
	ttl := time.Duration(DefaultLimits.InactivityTTLSeconds) * time.Second

	// Reads never count as activity.
	g.advance(ttl - 15*time.Minute)
	g.get("", 200)
	g.advance(5 * time.Minute) // 10 minutes before deactivation
	g.r.Sweep()
	p := g.get("", 200)
	if len(p.Messages) != 2 || event(t, p.Messages[1]) != "session_expiring" {
		t.Fatalf("no expiry warning: %+v", p)
	}
	g.r.Sweep()
	if p = g.get("", 200); len(p.Messages) != 2 {
		t.Fatal("expiry warning repeated")
	}

	// A write resets the clock and re-arms the warning.
	g.post("still here", nil)
	g.advance(ttl - 5*time.Minute)
	g.r.Sweep()
	if p = g.get("", 200); len(p.Messages) != 4 || event(t, p.Messages[3]) != "session_expiring" {
		t.Fatalf("warning not re-armed: %+v", p)
	}
	g.advance(5 * time.Minute)
	g.r.Sweep()
	g.do("POST", "/s/"+g.uid, g.key, map[string]any{"message": map[string]any{"role": "user", "name": "a", "content": "x"}}, 409, nil)
	g.get("", 200) // reads work during grace

	g.advance(time.Duration(DefaultLimits.GraceSeconds) * time.Second)
	g.r.Sweep()
	g.get("", 410)
	g.do("POST", "/s/"+g.uid+"/terminate", g.key, nil, 410, nil)
}

func TestCaps(t *testing.T) {
	lim := DefaultLimits
	lim.MessagesPerMinute = 3
	lim.MaxMessages = 10
	g := newRig(t, lim)
	for i := 0; i < 3; i++ {
		g.post("m", nil)
	}
	res := g.do("POST", "/s/"+g.uid, g.key, map[string]any{"message": map[string]any{"role": "user", "name": "a", "content": "x"}}, 429, nil)
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	g.advance(time.Minute)
	for i := 0; i < 3; i++ {
		g.post("m", nil)
	}
	g.advance(time.Minute)
	g.post("m", nil)
	g.post("m", nil)
	e := g.post("m", nil) // seq 9 = 90% of 10 → warning lands as seq 10
	if e.Seq != 9 {
		t.Fatalf("seq: %d", e.Seq)
	}
	p := g.get("?since=9", 200)
	if len(p.Messages) != 1 || event(t, p.Messages[0]) != "limit_warning" {
		t.Fatalf("no limit warning: %+v", p)
	}
	g.advance(time.Minute)
	var out struct{ Error errBody }
	g.do("POST", "/s/"+g.uid, g.key, map[string]any{"message": map[string]any{"role": "user", "name": "a", "content": "x"}}, 409, &out)
	if out.Error.Code != "limit_exceeded" {
		t.Fatalf("cap error: %+v", out)
	}
}

func TestSessionsPerIP(t *testing.T) {
	lim := DefaultLimits
	lim.SessionsPerIPPerHour = 2 // the rig already used one
	g := newRig(t, lim)
	g.do("POST", "/sessions", "", "", 201, nil)
	g.do("POST", "/sessions", "", "", 429, nil)
	g.advance(time.Hour)
	g.do("POST", "/sessions", "", "", 201, nil)
}
