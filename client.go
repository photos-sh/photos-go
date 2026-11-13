package photos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Version is the version of this library.
const Version = "1.0.0"

const (
	// DefaultBaseURL is where the API lives unless WithBaseURL or the
	// PHOTOS_BASE_URL environment variable says otherwise.
	DefaultBaseURL = "https://api.photos.sh"
	// DefaultTimeout is the time one HTTP attempt may take.
	DefaultTimeout = 120 * time.Second
	// DefaultRetries is how many times a request is sent again after a 503
	// or a connection failure.
	DefaultRetries = 2

	maxRetryDelay    = 30 * time.Second
	defaultUserAgent = "photos-sh-go/" + Version
)

// Client talks to the Photos.sh API. Make one with New and share it: it is
// safe for use by several goroutines at once.
type Client struct {
	// Jobs runs one operation on many images in the background.
	Jobs *JobsService

	apiKey     string
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
	retries    int
	userAgent  string

	// sleep waits between attempts and between polls, and now tells the
	// time for Jobs.Wait; tests replace them.
	sleep func(ctx context.Context, d time.Duration) error
	now   func() time.Time
}

// Option changes how New builds a Client.
type Option func(*Client) error

// WithBaseURL sets the address of the API, for example
// "http://localhost:8080". A trailing slash is ignored. It takes precedence
// over the PHOTOS_BASE_URL environment variable.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) error {
		if baseURL == "" {
			return errors.New("photos: WithBaseURL: empty URL")
		}
		c.baseURL = baseURL
		return nil
	}
}

// WithHTTPClient makes the Client send its requests with hc (for a proxy, a
// custom transport, or tracing). The Client's own timeout still applies to
// every attempt; see WithTimeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) error {
		if hc == nil {
			return errors.New("photos: WithHTTPClient: nil client")
		}
		c.httpClient = hc
		return nil
	}
}

// WithTimeout sets how long one HTTP attempt may take, from sending the
// request to reading the last byte of the response. The default is
// DefaultTimeout (120 s; model work takes seconds). Zero means no timeout
// beyond the context's own deadline.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if d < 0 {
			return errors.New("photos: WithTimeout: negative duration")
		}
		c.timeout = d
		return nil
	}
}

// WithRetries sets how many times a request is sent again after a 503
// response or a connection failure (default DefaultRetries). Zero turns
// retries off.
func WithRetries(n int) Option {
	return func(c *Client) error {
		if n < 0 {
			return errors.New("photos: WithRetries: negative count")
		}
		c.retries = n
		return nil
	}
}

// WithUserAgent puts a product token for your application in front of the
// library's own, giving "<userAgent> photos-sh-go/<version>".
func WithUserAgent(userAgent string) Option {
	return func(c *Client) error {
		if ua := strings.TrimSpace(userAgent); ua != "" {
			c.userAgent = ua + " " + defaultUserAgent
		}
		return nil
	}
}

// New returns a Client that signs its requests with apiKey. An empty apiKey
// falls back to the PHOTOS_API_KEY environment variable; with neither, New
// returns ErrMissingAPIKey.
//
// The base URL is DefaultBaseURL unless WithBaseURL or the PHOTOS_BASE_URL
// environment variable gives another.
func New(apiKey string, opts ...Option) (*Client, error) {
	c := &Client{
		apiKey:    apiKey,
		timeout:   DefaultTimeout,
		retries:   DefaultRetries,
		userAgent: defaultUserAgent,
		sleep:     sleepContext,
		now:       time.Now,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	if c.apiKey == "" {
		c.apiKey = os.Getenv("PHOTOS_API_KEY")
	}
	if c.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if c.baseURL == "" {
		c.baseURL = os.Getenv("PHOTOS_BASE_URL")
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	u, err := url.Parse(c.baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("photos: invalid base URL %q: want http(s)://host[/path]", c.baseURL)
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	c.Jobs = &JobsService{client: c}
	return c, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// request is one API call, which may be attempted several times.
type request struct {
	method string
	path   string // below the base URL, starting with "/"
	query  url.Values
	body   *requestBody
	noAuth bool
	// retryConnection allows sending again after a connection failure. It is
	// off for job creation, where the first attempt may have made the job.
	retryConnection bool
	// noRetry turns every retry off (health, where a 503 is an answer).
	noRetry bool
}

// requestBody makes the body of each attempt.
type requestBody struct {
	contentType string
	// open returns the body for one attempt and its length (-1: unknown).
	open func() (io.ReadCloser, int64, error)
	// replayable is false when open works only once (a plain io.Reader).
	replayable bool
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func jsonBody(v any) (*requestBody, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("photos: encoding the request: %w", err)
	}
	return &requestBody{
		contentType: "application/json",
		replayable:  true,
		open: func() (io.ReadCloser, int64, error) {
			return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
		},
	}, nil
}

// do sends r, retrying as configured, and returns the response whatever its
// status; an error means no response was read.
func (c *Client) do(ctx context.Context, r *request) (*response, error) {
	if ctx == nil {
		return nil, errors.New("photos: nil context")
	}
	for attempt := 0; ; attempt++ {
		resp, err := c.attempt(ctx, r)

		var delay time.Duration
		switch {
		case err == nil && retryableStatus(resp):
			delay = retryDelay(attempt, retryAfter(resp.header))
		case err != nil && r.retryConnection && isConnectionFailure(err):
			delay = retryDelay(attempt, 0)
		default:
			return resp, err
		}
		if r.noRetry || attempt >= c.retries || (r.body != nil && !r.body.replayable) {
			return resp, err
		}
		if serr := c.sleep(ctx, delay); serr != nil {
			return nil, serr
		}
	}
}

// retryableStatus reports whether a response asks to be tried again: a 503
// that says "busy", or one that is not the API's own error (a proxy in front
// of it). A 503 "model_unavailable" is permanent and is not retried.
func retryableStatus(resp *response) bool {
	if resp.status != http.StatusServiceUnavailable {
		return false
	}
	code := newAPIError(resp).Code
	return code == "" || code == CodeBusy
}

// isConnectionFailure reports whether err is a failure to get a response
// that is worth another attempt: not a timeout, not a failure after the
// response began.
func isConnectionFailure(err error) bool {
	var ce *ConnectionError
	return errors.As(err, &ce) && !ce.Timeout && !ce.afterResponse
}

// retryDelay is the wait before attempt+1: the server's Retry-After when it
// gave one, else 1 s, 2 s, 4 s…, never more than 30 s.
func retryDelay(attempt int, after time.Duration) time.Duration {
	d := after
	if d <= 0 {
		if attempt > 5 {
			attempt = 5
		}
		d = time.Second << attempt
	}
	if d > maxRetryDelay {
		d = maxRetryDelay
	}
	return d
}

// retryAfter reads a Retry-After header: seconds, or an HTTP date.
func retryAfter(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := time.ParseDuration(v + "s"); err == nil && secs > 0 {
		return secs
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func (c *Client) attempt(ctx context.Context, r *request) (*response, error) {
	actx := ctx
	if c.timeout > 0 {
		var cancel context.CancelFunc
		actx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	u := c.baseURL + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}

	var body io.ReadCloser
	var tracked *trackedBody
	length := int64(0)
	if r.body != nil {
		b, n, err := r.body.open()
		if err != nil {
			return nil, err
		}
		tracked = &trackedBody{ReadCloser: b}
		body, length = tracked, n
	}
	req, err := http.NewRequestWithContext(actx, r.method, u, body)
	if err != nil {
		if body != nil {
			body.Close()
		}
		return nil, fmt.Errorf("photos: %w", err)
	}
	if r.body != nil {
		req.ContentLength = length
		req.Header.Set("Content-Type", r.body.contentType)
		if r.body.replayable {
			open := r.body.open
			req.GetBody = func() (io.ReadCloser, error) {
				b, _, err := open()
				return b, err
			}
		}
	}
	req.Header.Set("User-Agent", c.userAgent)
	if !r.noAuth {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		if tracked != nil {
			if rerr := tracked.readErr(); rerr != nil {
				// The input could not be read: not the network's fault.
				return nil, fmt.Errorf("photos: reading the input: %w", rerr)
			}
		}
		return nil, c.transportError(ctx, actx, err, false)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, c.transportError(ctx, actx, err, true)
	}
	return &response{status: res.StatusCode, header: res.Header, body: data}, nil
}

// transportError turns a failed attempt into the error the caller sees: the
// context's own error when the caller cancelled or its deadline passed, else
// a *ConnectionError.
func (c *Client) transportError(ctx, actx context.Context, err error, afterResponse bool) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	// net/http wraps failures in *url.Error, whose text repeats the URL.
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	timeout := errors.Is(actx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
	if !timeout {
		var ne net.Error
		timeout = errors.As(err, &ne) && ne.Timeout()
	}
	return &ConnectionError{Err: err, Timeout: timeout, afterResponse: afterResponse}
}

// trackedBody remembers a failure to read the body, to tell an unreadable
// input from a broken connection.
type trackedBody struct {
	io.ReadCloser
	mu  sync.Mutex
	err error
}

func (t *trackedBody) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		t.mu.Lock()
		if t.err == nil {
			t.err = err
		}
		t.mu.Unlock()
	}
	return n, err
}

func (t *trackedBody) readErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// send does r and turns every status but 2xx into an *Error.
func (c *Client) send(ctx context.Context, r *request) (*response, error) {
	resp, err := c.do(ctx, r)
	if err != nil {
		return nil, err
	}
	if resp.status < 200 || resp.status > 299 {
		return nil, newAPIError(resp)
	}
	return resp, nil
}

// getJSON does a GET and decodes the JSON response into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.send(ctx, &request{method: http.MethodGet, path: path, retryConnection: true})
	if err != nil {
		return err
	}
	return decodeJSON(resp, out)
}

func decodeJSON(resp *response, out any) error {
	if err := json.Unmarshal(resp.body, out); err != nil {
		return fmt.Errorf("photos: decoding the response (HTTP %d): %w", resp.status, err)
	}
	return nil
}
