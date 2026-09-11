// Package client speaks the iris session protocol over plain HTTP.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Session is one endpoint: the session URL and its bearer key.
type Session struct {
	URL string `json:"url"`
	Key string `json:"key"`
}

// Result is a relay response: the status and the raw body.
type Result struct {
	Status int
	Body   []byte
}

// OK reports a 2xx status.
func (r Result) OK() bool { return r.Status/100 == 2 }

const pollSeconds = 55

var httpClient = &http.Client{Timeout: (pollSeconds + 15) * time.Second}

// Do sends one request under the session's path and key.
func (s Session) Do(ctx context.Context, method, path string, body io.Reader, contentType string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.URL+path, body)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}
	return Result{resp.StatusCode, b}, nil
}

// Post appends a message; body is the JSON {message, metadata?}.
func (s Session) Post(ctx context.Context, body []byte) (Result, error) {
	return s.Do(ctx, http.MethodPost, "", bytes.NewReader(body), "application/json")
}

// Read returns messages with seq > since, at most limit (0 = relay default).
func (s Session) Read(ctx context.Context, since, limit int) (Result, error) {
	q := url.Values{"since": {strconv.Itoa(since)}}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return s.Do(ctx, http.MethodGet, "?"+q.Encode(), nil, "")
}

// ErrTimeout is returned by Wait when ctx ends before a message lands.
var ErrTimeout = errors.New("no message before the deadline")

// Wait blocks until a message with seq > since lands (only urgent ones when
// urgent is set), the session refuses, or ctx ends. A relay that stops
// answering is retried briefly before the error is returned.
func (s Session) Wait(ctx context.Context, since int, urgent bool) (Result, error) {
	q := url.Values{"since": {strconv.Itoa(since)}, "timeout": {strconv.Itoa(pollSeconds)}}
	if urgent {
		q.Set("filter", "urgent")
	}
	path := "/wait?" + q.Encode()
	fails := 0
	for {
		r, err := s.Do(ctx, http.MethodGet, path, nil, "")
		switch {
		case ctx.Err() != nil:
			return Result{}, ErrTimeout
		case err != nil:
			if fails++; fails > 3 {
				return Result{}, err
			}
			if !sleep(ctx, 5*time.Second) {
				return Result{}, ErrTimeout
			}
			continue
		case r.Status == http.StatusNoContent:
			fails = 0
			continue
		default:
			return r, nil
		}
	}
}

// Put stores a file under name and returns the announcement envelope.
func (s Session) Put(ctx context.Context, name, contentType string, body io.Reader) (Result, error) {
	return s.Do(ctx, http.MethodPut, "/files/"+url.PathEscape(name), body, contentType)
}

// Get streams a file's bytes to w and returns the status. On a non-2xx
// status nothing is written and the error body is returned instead.
func (s Session) Get(ctx context.Context, name string, w io.Writer) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+"/files/"+url.PathEscape(name), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Key)
	resp, err := httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return Result{resp.StatusCode, b}, nil
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return Result{}, err
	}
	return Result{Status: resp.StatusCode}, nil
}

// Files lists the session's files.
func (s Session) Files(ctx context.Context) (Result, error) {
	return s.Do(ctx, http.MethodGet, "/files", nil, "")
}

// Terminate makes the session read-only.
func (s Session) Terminate(ctx context.Context) (Result, error) {
	return s.Do(ctx, http.MethodPost, "/terminate", nil, "")
}

// Post is a message to compose: text plus optional file parts and flags.
type Post struct {
	Role    string
	Name    string
	Text    string
	Files   []FilePart
	Urgent  bool
	Human   bool // attn: human
	ReplyTo int
	Extra   map[string]any // passed through under metadata; flags win on conflict
}

// FilePart references an uploaded file by name and announcement seq.
type FilePart struct {
	Name string `json:"name"`
	Seq  int    `json:"seq"`
}

// JSON encodes the post as the relay's {message, metadata} body.
func (p Post) JSON() ([]byte, error) {
	if p.Name == "" {
		return nil, errors.New("post: name is required")
	}
	if p.Text == "" && len(p.Files) == 0 {
		return nil, errors.New("post: content is empty")
	}
	role := p.Role
	if role == "" {
		role = "assistant"
	}
	var content any = p.Text
	if len(p.Files) > 0 {
		parts := []map[string]any{}
		if p.Text != "" {
			parts = append(parts, map[string]any{"type": "text", "text": p.Text})
		}
		for _, f := range p.Files {
			parts = append(parts, map[string]any{"type": "file", "file": f})
		}
		content = parts
	}
	meta := map[string]any{}
	for k, v := range p.Extra {
		meta[k] = v
	}
	if p.Urgent {
		meta["urgent"] = true
	}
	if p.Human {
		meta["attn"] = "human"
	}
	if p.ReplyTo > 0 {
		meta["reply_to"] = p.ReplyTo
	}
	body := map[string]any{"message": map[string]any{"role": role, "name": p.Name, "content": content}}
	if len(meta) > 0 {
		body["metadata"] = meta
	}
	return json.Marshal(body)
}

// Error extracts the relay's error message from a non-2xx body.
func (r Result) Error() string {
	var e struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if json.Unmarshal(r.Body, &e) == nil && e.Error.Code != "" {
		return fmt.Sprintf("%s: %s", e.Error.Code, e.Error.Message)
	}
	return fmt.Sprintf("http %d", r.Status)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
