package photos

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const (
	// SignatureHeader is the header a webhook's signature arrives in.
	SignatureHeader = "Photos-Signature"
	// DefaultWebhookTolerance is how far a webhook's timestamp may be from
	// now when VerifyWebhook is given no tolerance.
	DefaultWebhookTolerance = 5 * time.Minute
	// EventJobCompleted is the Type of the event sent when a job is done.
	EventJobCompleted = "job.completed"
)

// Event is a verified webhook delivery.
type Event struct {
	// Type is the kind of event: EventJobCompleted.
	Type string `json:"type"`
	// Job is the job the event is about.
	Job *Job `json:"job,omitempty"`
	// Raw is the event as it came.
	Raw json.RawMessage `json:"-"`
}

// WebhookError is why VerifyWebhook rejected a delivery. Compare with
// errors.Is against ErrWebhookHeader, ErrWebhookSignature,
// ErrWebhookTimestamp and ErrWebhookPayload, or catch them all with
// errors.As and a *WebhookError.
type WebhookError struct {
	Reason string
}

func (e *WebhookError) Error() string { return "photos: webhook: " + e.Reason }

// Why a webhook is rejected.
var (
	// ErrWebhookHeader: the Photos-Signature header is missing or malformed.
	ErrWebhookHeader error = &WebhookError{Reason: "missing or malformed Photos-Signature header"}
	// ErrWebhookSignature: the signature does not match the body and secret.
	ErrWebhookSignature error = &WebhookError{Reason: "signature does not match"}
	// ErrWebhookTimestamp: the timestamp is outside the tolerance.
	ErrWebhookTimestamp error = &WebhookError{Reason: "timestamp outside the tolerance"}
	// ErrWebhookPayload: the signature is right but the body is not an event.
	ErrWebhookPayload error = &WebhookError{Reason: "body is not a JSON event"}
)

// VerifyWebhook checks that a webhook delivery was signed by Photos.sh and
// returns the event it carries.
//
// body is the request body exactly as received (verify before decoding it),
// header the value of the Photos-Signature header ("t=<unix seconds>,v1=<hex>"),
// and secret your signing key (Client.WebhookSecret). Deliveries whose
// timestamp is more than tolerance from now are rejected; a tolerance of
// zero or less means DefaultWebhookTolerance.
//
// It needs no Client. An error is one of ErrWebhookHeader,
// ErrWebhookSignature, ErrWebhookTimestamp or ErrWebhookPayload.
func VerifyWebhook(body []byte, header, secret string, tolerance time.Duration) (*Event, error) {
	return VerifyWebhookAt(body, header, secret, tolerance, time.Now())
}

// VerifyWebhookAt is VerifyWebhook with the current time given as now, for
// tests and for verifying stored deliveries.
func VerifyWebhookAt(body []byte, header, secret string, tolerance time.Duration, now time.Time) (*Event, error) {
	if tolerance <= 0 {
		tolerance = DefaultWebhookTolerance
	}
	timestamp, signatures, ok := parseSignature(header)
	if !ok {
		return nil, ErrWebhookHeader
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{'.'})
	mac.Write(body)
	want := mac.Sum(nil)

	// Look at every candidate, so the time taken says nothing about which
	// one matched.
	match := false
	for _, sig := range signatures {
		if hmac.Equal(sig, want) {
			match = true
		}
	}
	if !match {
		return nil, ErrWebhookSignature
	}

	seconds, _ := strconv.ParseInt(timestamp, 10, 64)
	age := now.Sub(time.Unix(seconds, 0))
	if age < 0 {
		age = -age
	}
	if age > tolerance {
		return nil, ErrWebhookTimestamp
	}

	event := &Event{}
	if err := json.Unmarshal(body, event); err != nil || event.Type == "" {
		return nil, ErrWebhookPayload
	}
	event.Raw = append(json.RawMessage(nil), body...)
	return event, nil
}

// parseSignature reads "t=<unix seconds>,v1=<hex>[,v1=<hex>…]". Parts with
// other names are ignored, so the scheme can grow.
func parseSignature(header string) (timestamp string, signatures [][]byte, ok bool) {
	for _, part := range strings.Split(header, ",") {
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			return "", nil, false
		}
		switch name {
		case "t":
			if timestamp != "" {
				return "", nil, false
			}
			if _, err := strconv.ParseInt(value, 10, 64); err != nil || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
				return "", nil, false
			}
			timestamp = value
		case "v1":
			sig, err := hex.DecodeString(value)
			if err != nil || len(sig) != sha256.Size {
				return "", nil, false
			}
			signatures = append(signatures, sig)
		}
	}
	return timestamp, signatures, timestamp != "" && len(signatures) > 0
}
