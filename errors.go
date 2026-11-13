package photos

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Error codes the API answers with, in Error.Code.
const (
	CodeBadRequest        = "bad_request"        // 400: a parameter or the image is wrong
	CodeUnauthorized      = "unauthorized"       // 401: missing or invalid API key
	CodePaymentRequired   = "payment_required"   // 402: out of credits
	CodeAccountSuspended  = "account_suspended"  // 403
	CodeNotFound          = "not_found"          // 404: no such job or item result
	CodePayloadTooLarge   = "payload_too_large"  // 413
	CodeUnsupportedFormat = "unsupported_format" // 415
	CodeUnprocessable     = "unprocessable"      // 422: understood, but cannot be done
	CodeBusy              = "busy"               // 503: all workers busy; retried for you
	CodeModelUnavailable  = "model_unavailable"  // 503: not available on this server; never retried
	CodeInternal          = "internal"           // 500

	// CodeInputUnavailable appears only in a failed job item's error: the
	// input URL could not be fetched.
	CodeInputUnavailable = "input_unavailable"
)

// Error is an error response from the API. Get at it with errors.As, or
// test for one code with errors.Is and a sentinel:
//
//	if errors.Is(err, photos.ErrPaymentRequired) { … }
//
//	var apiErr *photos.Error
//	if errors.As(err, &apiErr) { log.Print(apiErr.Status, apiErr.Code) }
type Error struct {
	// Status is the HTTP status.
	Status int
	// Code is the API's error code (one of the Code constants; the API may
	// add more). It is empty when the response was not the API's JSON, as
	// from a proxy in front of it.
	Code string
	// Message says what went wrong. For a response that was not the API's
	// JSON it is the start of the body.
	Message string
	// RetryAfter is the Retry-After header, zero when the response had none.
	RetryAfter time.Duration
	// Header is the response's headers.
	Header http.Header
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("photos: ")
	if e.Code != "" {
		b.WriteString(e.Code)
		b.WriteString(": ")
	}
	if e.Message != "" {
		b.WriteString(e.Message)
	} else {
		b.WriteString(http.StatusText(e.Status))
	}
	if e.Status != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.Status)
	}
	return b.String()
}

// Is makes errors.Is(err, photos.ErrUnauthorized) and the other sentinels
// work: an Error matches a sentinel with the same code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok || t == nil {
		return false
	}
	if t.Status != 0 && t.Status != e.Status {
		return false
	}
	return t.Code == e.Code && (t.Message == "" || t.Message == e.Message)
}

// Sentinels for errors.Is, one per error code.
var (
	ErrBadRequest        error = &Error{Code: CodeBadRequest}
	ErrUnauthorized      error = &Error{Code: CodeUnauthorized}
	ErrPaymentRequired   error = &Error{Code: CodePaymentRequired}
	ErrAccountSuspended  error = &Error{Code: CodeAccountSuspended}
	ErrNotFound          error = &Error{Code: CodeNotFound}
	ErrPayloadTooLarge   error = &Error{Code: CodePayloadTooLarge}
	ErrUnsupportedFormat error = &Error{Code: CodeUnsupportedFormat}
	ErrUnprocessable     error = &Error{Code: CodeUnprocessable}
	ErrBusy              error = &Error{Code: CodeBusy}
	ErrModelUnavailable  error = &Error{Code: CodeModelUnavailable}
	ErrInternal          error = &Error{Code: CodeInternal}
)

// Errors that are not responses from the API.
var (
	// ErrMissingAPIKey is returned by New when no key was given and
	// PHOTOS_API_KEY is not set.
	ErrMissingAPIKey = errors.New("photos: no API key: pass one to New or set PHOTOS_API_KEY")
	// ErrWaitTimeout is returned (wrapped) by Jobs.Wait when the job did not
	// complete within WaitOptions.Timeout.
	ErrWaitTimeout = errors.New("photos: timed out waiting for the job")
	// ErrMissingResultHeaders is returned (wrapped) by JobItemResult.RemoveText,
	// JobItemResult.Fix and the other typed results of a job item when the
	// result does not carry the headers they read: it is the result of
	// another operation, or the engine is too old to keep those headers on
	// job items.
	ErrMissingResultHeaders = errors.New("photos: the job item result lacks the headers")
)

// ConnectionError means no (complete) response arrived: the connection could
// not be made or broke, or the Client's timeout passed. Unwrap gives the
// cause.
//
// A cancelled context, or one whose own deadline passed, is not reported as a
// ConnectionError: the call returns context.Canceled or
// context.DeadlineExceeded.
type ConnectionError struct {
	// Err is the cause, from net/http.
	Err error
	// Timeout is true when the Client's timeout (WithTimeout) passed, or the
	// network reported a timeout. Timeouts are not retried.
	Timeout bool

	afterResponse bool
}

func (e *ConnectionError) Error() string {
	if e.Timeout {
		return fmt.Sprintf("photos: request timed out: %v", e.Err)
	}
	return fmt.Sprintf("photos: connection failed: %v", e.Err)
}

func (e *ConnectionError) Unwrap() error { return e.Err }

// newAPIError reads an error response:
// {"error": {"code": "...", "message": "..."}}, or whatever came instead.
func newAPIError(resp *response) *Error {
	e := &Error{
		Status:     resp.status,
		RetryAfter: retryAfter(resp.header),
		Header:     resp.header,
	}
	var envelope struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.body, &envelope); err == nil && envelope.Error != nil &&
		(envelope.Error.Code != "" || envelope.Error.Message != "") {
		e.Code = envelope.Error.Code
		e.Message = envelope.Error.Message
		return e
	}
	e.Message = bodyStart(resp.body)
	return e
}

// bodyStart is the first few hundred characters of a body, on one line.
func bodyStart(body []byte) string {
	const limit = 300
	s := strings.Join(strings.Fields(strings.ToValidUTF8(string(body), "\uFFFD")), " ")
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
