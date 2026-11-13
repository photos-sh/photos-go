package photos

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewAPIKey(t *testing.T) {
	t.Setenv("PHOTOS_API_KEY", "")
	t.Setenv("PHOTOS_BASE_URL", "")

	if _, err := New(""); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("New without a key: err = %v, want ErrMissingAPIKey", err)
	}

	t.Setenv("PHOTOS_API_KEY", "from-env")
	c, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if c.apiKey != "from-env" {
		t.Errorf("apiKey = %q, want the environment's", c.apiKey)
	}
	if c.baseURL != DefaultBaseURL || c.timeout != 120*time.Second || c.retries != 2 {
		t.Errorf("defaults = %q, %v, %d", c.baseURL, c.timeout, c.retries)
	}
	if c.Jobs == nil {
		t.Error("Jobs is nil")
	}

	c, err = New("explicit")
	if err != nil {
		t.Fatal(err)
	}
	if c.apiKey != "explicit" {
		t.Errorf("apiKey = %q, want the argument", c.apiKey)
	}
}

func TestNewBaseURL(t *testing.T) {
	t.Setenv("PHOTOS_BASE_URL", "http://env.example:8080/api/")
	c, err := New("k")
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "http://env.example:8080/api" {
		t.Errorf("baseURL = %q, want the environment's without the slash", c.baseURL)
	}

	c, err = New("k", WithBaseURL("http://opt.example//"))
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "http://opt.example" {
		t.Errorf("baseURL = %q, want the option's", c.baseURL)
	}

	for _, bad := range []string{"photos.sh/api", "ftp://photos.sh", "http://"} {
		if _, err := New("k", WithBaseURL(bad)); err == nil {
			t.Errorf("WithBaseURL(%q): no error", bad)
		}
	}
}

func TestNewRejectsBadOptions(t *testing.T) {
	for name, opt := range map[string]Option{
		"empty base URL":   WithBaseURL(""),
		"nil HTTP client":  WithHTTPClient(nil),
		"negative timeout": WithTimeout(-time.Second),
		"negative retries": WithRetries(-1),
	} {
		if _, err := New("k", opt); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRequestHeaders(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"account":"acme","unlimited":true}`)
	})
	if _, err := f.client.Account(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r.Method != "GET" || r.Path != "/api/v1/account" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization = %q", got)
	}
	if got := r.Header.Get("User-Agent"); got != "photos-sh-go/"+Version {
		t.Errorf("User-Agent = %q", got)
	}
}

func TestWithUserAgentAndHTTPClient(t *testing.T) {
	var used atomic.Int32
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		used.Add(1)
		return http.DefaultTransport.RoundTrip(r)
	})}
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `[]`)
	}, WithUserAgent("shop/2.1"), WithHTTPClient(hc))
	if _, err := f.client.Presets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.last().Header.Get("User-Agent"); got != "shop/2.1 photos-sh-go/"+Version {
		t.Errorf("User-Agent = %q", got)
	}
	if used.Load() != 1 {
		t.Errorf("the supplied HTTP client was used %d times, want 1", used.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRetryOn503(t *testing.T) {
	var n atomic.Int32
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		switch n.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "5")
			writeError(w, 503, "busy", "all workers are busy; retry shortly")
		case 2:
			writeError(w, 503, "busy", "all workers are busy; retry shortly")
		default:
			writeImage(w, "image/png", []byte("PNG"), nil)
		}
	})
	png := []byte("an image")
	img, err := f.client.Resize(context.Background(), Bytes("a.png", png), &ResizeOptions{Width: 10})
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Bytes) != "PNG" {
		t.Errorf("body = %q", img.Bytes)
	}
	if f.count() != 3 {
		t.Fatalf("%d requests, want 3", f.count())
	}
	// Retry-After first; without one, the second wait is 2 s.
	if got, want := f.slept(), []time.Duration{5 * time.Second, 2 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	// Every attempt carried the whole image.
	for i, r := range f.requests {
		if p := r.part("image"); p == nil || !bytes.Equal(p.Data, png) {
			t.Errorf("attempt %d: image part = %+v", i+1, p)
		}
	}
}

func TestRetryDelays(t *testing.T) {
	for _, tc := range []struct {
		retryAfter string
		want       []time.Duration
	}{
		{"", []time.Duration{time.Second, 2 * time.Second}},
		{"120", []time.Duration{30 * time.Second, 30 * time.Second}},
		{"nonsense", []time.Duration{time.Second, 2 * time.Second}},
		{"0", []time.Duration{time.Second, 2 * time.Second}},
	} {
		f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
			if tc.retryAfter != "" {
				w.Header().Set("Retry-After", tc.retryAfter)
			}
			writeError(w, 503, "busy", "busy")
		})
		_, err := f.client.Account(context.Background())
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Status != 503 || !errors.Is(err, ErrBusy) {
			t.Fatalf("Retry-After %q: err = %v, want a busy *Error", tc.retryAfter, err)
		}
		if f.count() != 3 {
			t.Errorf("Retry-After %q: %d requests, want 3", tc.retryAfter, f.count())
		}
		if got := f.slept(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Retry-After %q: waits = %v, want %v", tc.retryAfter, got, tc.want)
		}
	}
}

func TestRetryOnlyBusy503(t *testing.T) {
	for name, tc := range map[string]struct {
		answer       func(w http.ResponseWriter)
		wantRequests int
	}{
		"busy":              {func(w http.ResponseWriter) { writeError(w, 503, "busy", "busy") }, 3},
		"model_unavailable": {func(w http.ResponseWriter) { writeError(w, 503, "model_unavailable", "not here") }, 1},
		"another code":      {func(w http.ResponseWriter) { writeError(w, 503, "something_new", "?") }, 1},
		"a proxy's page": {func(w http.ResponseWriter) {
			w.WriteHeader(503)
			w.Write([]byte("<html>Service Unavailable</html>"))
		}, 3},
		"an empty body": {func(w http.ResponseWriter) { w.WriteHeader(503) }, 3},
	} {
		f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { tc.answer(w) })
		_, err := f.client.Account(context.Background())
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Status != 503 {
			t.Fatalf("%s: err = %v", name, err)
		}
		if f.count() != tc.wantRequests {
			t.Errorf("%s: %d requests, want %d", name, f.count(), tc.wantRequests)
		}
	}
}

func TestUnknownRouteIsAPlainAPIError(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { w.WriteHeader(404) })
	_, err := f.client.Account(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || apiErr.Code != "" {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("a 404 without the API's code matches ErrNotFound")
	}
	if got := err.Error(); got != "photos: Not Found (HTTP 404)" {
		t.Errorf("message = %q", got)
	}
}

func TestNoRetryOnOtherStatuses(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404, 413, 415, 422, 429, 500, 502, 504} {
		f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
			writeError(w, status, "internal", "no")
		})
		if _, err := f.client.Account(context.Background()); err == nil {
			t.Errorf("status %d: no error", status)
		}
		if f.count() != 1 || len(f.slept()) != 0 {
			t.Errorf("status %d: %d requests, waits %v; want one request", status, f.count(), f.slept())
		}
	}
}

func TestWithRetriesZero(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeError(w, 503, "busy", "busy")
	}, WithRetries(0))
	if _, err := f.client.Account(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if f.count() != 1 {
		t.Errorf("%d requests, want 1", f.count())
	}
}

// failingTransport fails the first n round trips as a refused connection
// would, before any request is sent.
func failingTransport(n int32, attempts *atomic.Int32) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if attempts.Add(1) <= n {
			if r.Body != nil {
				r.Body.Close()
			}
			return nil, errors.New("dial tcp: connection refused")
		}
		return http.DefaultTransport.RoundTrip(r)
	})
}

func TestRetryOnConnectionFailure(t *testing.T) {
	var attempts atomic.Int32
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/png", []byte("ok"), nil)
	}, WithHTTPClient(&http.Client{Transport: failingTransport(2, &attempts)}))

	if _, err := f.client.Resize(context.Background(), Bytes("a.png", []byte("x")), nil); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 || f.count() != 1 {
		t.Errorf("%d attempts, %d reached the server; want 3 and 1", attempts.Load(), f.count())
	}
	if got, want := f.slept(), []time.Duration{time.Second, 2 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
}

func TestConnectionError(t *testing.T) {
	var attempts atomic.Int32
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {},
		WithHTTPClient(&http.Client{Transport: failingTransport(100, &attempts)}))

	_, err := f.client.Account(context.Background())
	var ce *ConnectionError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v, want a *ConnectionError", err, err)
	}
	if ce.Timeout {
		t.Error("Timeout = true for a refused connection")
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		t.Error("a connection failure is also an *Error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("message = %q", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("%d attempts, want 3", attempts.Load())
	}
}

func TestTimeoutIsConnectionErrorAndNotRetried(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		<-release
	}, WithTimeout(40*time.Millisecond))
	defer close(release)

	_, err := f.client.Account(context.Background())
	var ce *ConnectionError
	if !errors.As(err, &ce) || !ce.Timeout {
		t.Fatalf("err = %T %v, want a *ConnectionError with Timeout", err, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false for %v", err)
	}
	if f.count() != 1 {
		t.Errorf("%d requests, want 1: timeouts are not retried", f.count())
	}
}

func TestContextErrorsPassThrough(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		<-release
	})
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for f.count() == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	_, err := f.client.Account(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: err = %v, want context.Canceled", err)
	}
	var ce *ConnectionError
	if errors.As(err, &ce) {
		t.Errorf("cancelled: err is a *ConnectionError: %v", err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = f.client.Account(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline: err = %v, want context.DeadlineExceeded", err)
	}
	if errors.As(err, &ce) {
		t.Errorf("deadline: err is a *ConnectionError: %v", err)
	}
	if f.count() != 2 {
		t.Errorf("%d requests, want 2: a done context is not retried", f.count())
	}
}

func TestContextCancelledWhileWaitingToRetry(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeError(w, 503, "busy", "busy")
	})
	ctx, cancel := context.WithCancel(context.Background())
	f.client.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := f.client.Account(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if f.count() != 1 {
		t.Errorf("%d requests, want 1", f.count())
	}
}

func TestAPIErrors(t *testing.T) {
	cases := []struct {
		status   int
		code     string
		sentinel error
	}{
		{400, CodeBadRequest, ErrBadRequest},
		{401, CodeUnauthorized, ErrUnauthorized},
		{402, CodePaymentRequired, ErrPaymentRequired},
		{403, CodeAccountSuspended, ErrAccountSuspended},
		{404, CodeNotFound, ErrNotFound},
		{413, CodePayloadTooLarge, ErrPayloadTooLarge},
		{415, CodeUnsupportedFormat, ErrUnsupportedFormat},
		{422, CodeUnprocessable, ErrUnprocessable},
		{500, CodeInternal, ErrInternal},
		{503, CodeBusy, ErrBusy},
		{503, CodeModelUnavailable, ErrModelUnavailable},
	}
	for _, tc := range cases {
		f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
			if tc.code == CodeBusy {
				w.Header().Set("Retry-After", "7")
			}
			writeError(w, tc.status, tc.code, "the message")
		}, WithRetries(0))
		_, err := f.client.Resize(context.Background(), Bytes("a.png", []byte("x")), nil)

		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("%s: err = %T %v, want an *Error", tc.code, err, err)
		}
		if apiErr.Status != tc.status || apiErr.Code != tc.code || apiErr.Message != "the message" {
			t.Errorf("%s: got %+v", tc.code, apiErr)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("%s: errors.Is(err, sentinel) = false", tc.code)
		}
		for _, other := range cases {
			if other.code != tc.code && errors.Is(err, other.sentinel) {
				t.Errorf("%s: also matches the sentinel for %s", tc.code, other.code)
			}
		}
		wantAfter := time.Duration(0)
		if tc.code == CodeBusy {
			wantAfter = 7 * time.Second
		}
		if apiErr.RetryAfter != wantAfter {
			t.Errorf("%s: RetryAfter = %v, want %v", tc.code, apiErr.RetryAfter, wantAfter)
		}
		if want := "photos: " + tc.code + ": the message"; !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%s: message = %q", tc.code, err.Error())
		}
	}
}

func TestNonJSONErrorBody(t *testing.T) {
	html := "<html>\n<body>Bad   Gateway " + strings.Repeat("é", 400) + "</body></html>"
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(502)
		w.Write([]byte(html))
	})
	_, err := f.client.Account(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v, want an *Error", err, err)
	}
	if apiErr.Status != 502 || apiErr.Code != "" {
		t.Errorf("got status %d code %q", apiErr.Status, apiErr.Code)
	}
	if !strings.HasPrefix(apiErr.Message, "<html> <body>Bad Gateway é") || !strings.HasSuffix(apiErr.Message, "…") {
		t.Errorf("message = %q", apiErr.Message)
	}
	if n := len([]rune(apiErr.Message)); n > 301 {
		t.Errorf("message is %d characters long", n)
	}
	for _, sentinel := range []error{ErrInternal, ErrBusy, ErrBadRequest} {
		if errors.Is(err, sentinel) {
			t.Errorf("an error without a code matches %v", sentinel)
		}
	}

	// JSON, but not the API's: still an *Error, with the body as message.
	f = newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 500, `{"detail":"oops"}`)
	})
	_, err = f.client.Account(context.Background())
	if !errors.As(err, &apiErr) || apiErr.Message != `{"detail":"oops"}` || apiErr.Code != "" {
		t.Errorf("err = %v", err)
	}
}

func TestBadJSONInSuccessfulResponse(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `not json`)
	})
	_, err := f.client.Account(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decoding the response") {
		t.Errorf("err = %v", err)
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		t.Error("a decoding failure is reported as an API error")
	}
}

func TestStreamingBodies(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/png", []byte("ok"), nil)
	})
	ctx := context.Background()
	data := bytes.Repeat([]byte("0123456789abcdef"), 20_000) // 320 kB
	path := filepath.Join(t.TempDir(), "photo one.jpg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	for name, tc := range map[string]struct {
		in         Input
		fileName   string
		wantLength bool
	}{
		"file":            {File(path), "photo one.jpg", true},
		"bytes":           {Bytes("b.jpg", data), "b.jpg", true},
		"bytes, no name":  {Bytes("", data), "image", true},
		"seekable reader": {Reader("f.jpg", file), "f.jpg", true},
		"bytes.Buffer":    {Reader("buf.jpg", bytes.NewBuffer(data)), "buf.jpg", true},
		"plain reader":    {Reader("r.jpg", onlyReader{bytes.NewReader(data)}), "r.jpg", false},
	} {
		if _, err := f.client.Resize(ctx, tc.in, nil); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r := f.last()
		p := r.part("image")
		if p == nil || !bytes.Equal(p.Data, data) {
			t.Fatalf("%s: the image did not arrive whole", name)
		}
		if p.FileName != tc.fileName {
			t.Errorf("%s: file name = %q, want %q", name, p.FileName, tc.fileName)
		}
		if len(r.Parts) != 1 {
			t.Errorf("%s: %d parts, want 1", name, len(r.Parts))
		}
		if tc.wantLength && r.ContentLength != int64(len(r.Body)) {
			t.Errorf("%s: Content-Length = %d, body is %d bytes", name, r.ContentLength, len(r.Body))
		}
		if !tc.wantLength && r.ContentLength != -1 {
			t.Errorf("%s: Content-Length = %d, want a chunked body", name, r.ContentLength)
		}
	}
}

func TestRetriesAndInputKinds(t *testing.T) {
	data := []byte("the image bytes")
	path := filepath.Join(t.TempDir(), "p.jpg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		in           func() Input
		wantRequests int
	}{
		"file":            {func() Input { return File(path) }, 3},
		"bytes":           {func() Input { return Bytes("b", data) }, 3},
		"seekable reader": {func() Input { return Reader("s", bytes.NewReader(data)) }, 3},
		// A reader that cannot rewind is sent once.
		"plain reader": {func() Input { return Reader("r", onlyReader{bytes.NewReader(data)}) }, 1},
	} {
		f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
			writeError(w, 503, "busy", "busy")
		})
		_, err := f.client.Resize(context.Background(), tc.in(), nil)
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("%s: err = %v, want busy", name, err)
		}
		if f.count() != tc.wantRequests {
			t.Errorf("%s: %d requests, want %d", name, f.count(), tc.wantRequests)
		}
		for i, r := range f.requests {
			if p := r.part("image"); p == nil || !bytes.Equal(p.Data, data) {
				t.Errorf("%s: attempt %d did not carry the whole image", name, i+1)
			}
		}
	}
}

func TestSeekableReaderIsSentFromItsPosition(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/png", nil, nil)
	})
	r := strings.NewReader("skip:payload")
	if _, err := r.Seek(5, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Resize(context.Background(), Reader("x", r), nil); err != nil {
		t.Fatal(err)
	}
	if got := string(f.last().part("image").Data); got != "payload" {
		t.Errorf("sent %q, want what was left to read", got)
	}
}

func TestPlainReaderCannotBeSentTwice(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/png", nil, nil)
	})
	in := Reader("x", plainReader("data"))
	if _, err := f.client.Resize(context.Background(), in, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Resize(context.Background(), in, nil); err == nil || !strings.Contains(err.Error(), "already sent") {
		t.Errorf("second use: err = %v", err)
	}
	if f.count() != 1 {
		t.Errorf("%d requests, want 1", f.count())
	}
}

func TestInputErrorsBeforeSending(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/png", nil, nil)
	})
	ctx := context.Background()

	_, err := f.client.Resize(ctx, File(filepath.Join(t.TempDir(), "missing.jpg")), nil)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: err = %v, want fs.ErrNotExist", err)
	}
	if _, err := f.client.Resize(ctx, File(t.TempDir()), nil); err == nil {
		t.Error("a directory: no error")
	}
	if _, err := f.client.Resize(ctx, Input{}, nil); err == nil {
		t.Error("zero Input: no error")
	}
	if _, err := f.client.Resize(ctx, Reader("x", nil), nil); err == nil {
		t.Error("nil reader: no error")
	}
	if _, err := f.client.Resize(ctx, File(""), nil); err == nil {
		t.Error("empty path: no error")
	}
	if f.count() != 0 {
		t.Errorf("%d requests were sent", f.count())
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func TestUnreadableInputIsNotAConnectionError(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/png", nil, nil)
	})
	f.lenient.Store(true)
	_, err := f.client.Resize(context.Background(), Reader("x", brokenReader{}), nil)
	if err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("err = %v", err)
	}
	var ce *ConnectionError
	if errors.As(err, &ce) {
		t.Errorf("an unreadable input is reported as a connection failure: %v", err)
	}
}

func TestHealth(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{200, `{"status":"ok"}`, HealthOK},
		{503, `{"status":"loading"}`, HealthLoading},
		{503, `{"status":"failed"}`, HealthFailed},
	} {
		f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
			writeJSON(w, tc.status, tc.body)
		})
		got, err := f.client.Health(context.Background())
		if err != nil || got != tc.want {
			t.Errorf("Health = %q, %v; want %q", got, err, tc.want)
		}
		r := f.last()
		if r.Method != "GET" || r.Path != "/api/health" {
			t.Errorf("request = %s %s", r.Method, r.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("Health sent the API key")
		}
		if f.count() != 1 {
			t.Errorf("%d requests, want 1: a 503 from health is an answer", f.count())
		}
	}

	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		w.WriteHeader(503)
		w.Write([]byte("upstream down"))
	})
	_, err := f.client.Health(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 503 || apiErr.Message != "upstream down" {
		t.Errorf("a 503 that is not the API's: err = %v", err)
	}
}
