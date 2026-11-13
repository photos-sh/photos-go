package photos

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "test-key-123"

// recordedPart is one part of a multipart request as the server saw it.
type recordedPart struct {
	Name        string
	FileName    string
	ContentType string
	Data        []byte
}

// recordedRequest is a request as the fake server saw it.
type recordedRequest struct {
	Method        string
	Path          string
	Query         url.Values
	Header        http.Header
	ContentLength int64
	Body          []byte
	Parts         []recordedPart
}

func (r *recordedRequest) part(name string) *recordedPart {
	for i := range r.Parts {
		if r.Parts[i].Name == name {
			return &r.Parts[i]
		}
	}
	return nil
}

func (r *recordedRequest) parts(name string) []recordedPart {
	var out []recordedPart
	for _, p := range r.Parts {
		if p.Name == name {
			out = append(out, p)
		}
	}
	return out
}

// fake is an in-process API: it records every request and answers with
// handler.
type fake struct {
	t      *testing.T
	server *httptest.Server
	client *Client
	// lenient: requests may arrive broken (a test of a failing upload).
	lenient atomic.Bool

	mu       sync.Mutex
	requests []*recordedRequest
	sleeps   []time.Duration
	clock    time.Time
}

// newFake starts a fake API and a Client for it whose waits are recorded
// instead of slept.
func newFake(t *testing.T, handler func(w http.ResponseWriter, r *recordedRequest), opts ...Option) *fake {
	t.Helper()
	f := &fake{t: t, clock: time.Unix(1_800_000_000, 0)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := record(f, r)
		f.mu.Lock()
		f.requests = append(f.requests, rec)
		f.mu.Unlock()
		handler(w, rec)
	}))
	t.Cleanup(f.server.Close)

	opts = append([]Option{WithBaseURL(f.server.URL + "/api/")}, opts...)
	c, err := New(testKey, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.sleep = func(ctx context.Context, d time.Duration) error {
		f.mu.Lock()
		f.sleeps = append(f.sleeps, d)
		f.clock = f.clock.Add(d)
		f.mu.Unlock()
		return ctx.Err()
	}
	c.now = func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.clock
	}
	f.client = c
	return f
}

func record(f *fake, r *http.Request) *recordedRequest {
	t := f.t
	body, err := io.ReadAll(r.Body)
	if f.lenient.Load() {
		return &recordedRequest{Method: r.Method, Path: r.URL.Path, Body: body}
	}
	if err != nil {
		t.Errorf("fake: reading the body: %v", err)
	}
	rec := &recordedRequest{
		Method:        r.Method,
		Path:          r.URL.Path,
		Query:         r.URL.Query(),
		Header:        r.Header.Clone(),
		ContentLength: r.ContentLength,
		Body:          body,
	}
	mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == "multipart/form-data" {
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("fake: bad multipart body: %v", err)
				break
			}
			data, _ := io.ReadAll(p)
			rec.Parts = append(rec.Parts, recordedPart{
				Name:        p.FormName(),
				FileName:    p.FileName(),
				ContentType: p.Header.Get("Content-Type"),
				Data:        data,
			})
		}
	}
	return rec
}

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// last returns the most recent request.
func (f *fake) last() *recordedRequest {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		f.t.Fatal("no request reached the fake API")
	}
	return f.requests[len(f.requests)-1]
}

func (f *fake) slept() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.sleeps...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	switch v := v.(type) {
	case string:
		io.WriteString(w, v)
	default:
		json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// writeImage answers like an image endpoint.
func writeImage(w http.ResponseWriter, contentType string, body []byte, headers map[string]string) {
	w.Header().Set("Content-Type", contentType)
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.Write(body)
}

func wantQuery(t *testing.T, got url.Values, want string) {
	t.Helper()
	w, err := url.ParseQuery(want)
	if err != nil {
		t.Fatal(err)
	}
	if got.Encode() != w.Encode() {
		t.Errorf("query = %q, want %q", got.Encode(), w.Encode())
	}
}

// onlyReader hides every method of a reader but Read.
type onlyReader struct{ r io.Reader }

func (o onlyReader) Read(p []byte) (int, error) { return o.r.Read(p) }

func plainReader(s string) io.Reader { return onlyReader{strings.NewReader(s)} }
