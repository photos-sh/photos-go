package photos

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const webhookSecret = "whsec_0123456789abcdef"

var webhookBody = []byte(`{"type":"job.completed","job":{"id":"job_abc","operation":"resize","params":"width=100","status":"completed","created_at":1800000000,"finished_at":1800000042,"items":[{"input":"a.jpg","status":"done","result":"/api/v1/jobs/job_abc/items/0"}],"counts":{"total":1,"done":1,"failed":0,"pending":0}}}`)

func sign(secret string, t int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", t)
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil)))
}

func TestVerifyWebhook(t *testing.T) {
	now := time.Unix(1_800_000_100, 0)
	header := sign(webhookSecret, now.Unix()-10, webhookBody)

	event, err := VerifyWebhookAt(webhookBody, header, webhookSecret, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != EventJobCompleted || event.Job == nil || event.Job.ID != "job_abc" || !event.Job.Completed() {
		t.Errorf("event = %+v", event)
	}
	if event.Job.Counts.Done != 1 || event.Job.Items[0].Result != "/api/v1/jobs/job_abc/items/0" {
		t.Errorf("job = %+v", event.Job)
	}
	if string(event.Raw) != string(webhookBody) {
		t.Error("Raw is not the body")
	}

}

// A vector made with another tool (openssl dgst -sha256 -hmac), so the test
// does not only agree with itself.
func TestVerifyWebhookKnownVector(t *testing.T) {
	body := []byte(`{"type":"job.completed","job":{"id":"job_1"}}`)
	const header = "t=1800000000,v1=90c1672761ea299dc1569349a086c9d9c3053d8fa0636d77d40a0286ce3fb226"
	event, err := VerifyWebhookAt(body, header, "whsec_test", 0, time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if event.Job.ID != "job_1" {
		t.Errorf("event = %+v", event)
	}
}

func TestVerifyWebhookUsesTheClock(t *testing.T) {
	header := sign(webhookSecret, time.Now().Unix(), webhookBody)
	if _, err := VerifyWebhook(webhookBody, header, webhookSecret, time.Minute); err != nil {
		t.Errorf("a fresh delivery: %v", err)
	}
	old := sign(webhookSecret, time.Now().Add(-time.Hour).Unix(), webhookBody)
	if _, err := VerifyWebhook(webhookBody, old, webhookSecret, time.Minute); !errors.Is(err, ErrWebhookTimestamp) {
		t.Errorf("an hour-old delivery: err = %v", err)
	}
}

func TestVerifyWebhookRejects(t *testing.T) {
	now := time.Unix(1_800_000_100, 0)
	ts := now.Unix()
	good := sign(webhookSecret, ts, webhookBody)
	sig := strings.TrimPrefix(good, fmt.Sprintf("t=%d,v1=", ts))

	tampered := append([]byte(nil), webhookBody...)
	tampered[len(tampered)-5] ^= 1

	for name, tc := range map[string]struct {
		body      []byte
		header    string
		secret    string
		tolerance time.Duration
		want      error
	}{
		"tampered body":     {tampered, good, webhookSecret, 0, ErrWebhookSignature},
		"wrong secret":      {webhookBody, good, "whsec_other", 0, ErrWebhookSignature},
		"empty secret":      {webhookBody, good, "", 0, ErrWebhookSignature},
		"changed timestamp": {webhookBody, fmt.Sprintf("t=%d,v1=%s", ts-1, sig), webhookSecret, 0, ErrWebhookSignature},
		"old timestamp":     {webhookBody, sign(webhookSecret, ts-301, webhookBody), webhookSecret, 0, ErrWebhookTimestamp},
		"future timestamp":  {webhookBody, sign(webhookSecret, ts+301, webhookBody), webhookSecret, 0, ErrWebhookTimestamp},
		"tighter tolerance": {webhookBody, sign(webhookSecret, ts-61, webhookBody), webhookSecret, time.Minute, ErrWebhookTimestamp},
		"empty header":      {webhookBody, "", webhookSecret, 0, ErrWebhookHeader},
		"no timestamp":      {webhookBody, "v1=" + sig, webhookSecret, 0, ErrWebhookHeader},
		"no signature":      {webhookBody, fmt.Sprintf("t=%d", ts), webhookSecret, 0, ErrWebhookHeader},
		"text timestamp":    {webhookBody, "t=now,v1=" + sig, webhookSecret, 0, ErrWebhookHeader},
		"signed timestamp":  {webhookBody, fmt.Sprintf("t=+%d,v1=%s", ts, sig), webhookSecret, 0, ErrWebhookHeader},
		"two timestamps":    {webhookBody, fmt.Sprintf("t=%d,t=%d,v1=%s", ts, ts, sig), webhookSecret, 0, ErrWebhookHeader},
		"not hex":           {webhookBody, fmt.Sprintf("t=%d,v1=zz%s", ts, sig[2:]), webhookSecret, 0, ErrWebhookHeader},
		"short signature":   {webhookBody, fmt.Sprintf("t=%d,v1=%s", ts, sig[:32]), webhookSecret, 0, ErrWebhookHeader},
		"no equals sign":    {webhookBody, "garbage", webhookSecret, 0, ErrWebhookHeader},
		"other scheme only": {webhookBody, fmt.Sprintf("t=%d,v0=%s", ts, sig), webhookSecret, 0, ErrWebhookHeader},
	} {
		event, err := VerifyWebhookAt(tc.body, tc.header, tc.secret, tc.tolerance, now)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
		if event != nil {
			t.Errorf("%s: an event was returned", name)
		}
		var we *WebhookError
		if !errors.As(err, &we) {
			t.Errorf("%s: err is not a *WebhookError", name)
		}
	}
}

func TestVerifyWebhookAccepts(t *testing.T) {
	now := time.Unix(1_800_000_100, 0)
	ts := now.Unix()
	good := sign(webhookSecret, ts, webhookBody)
	sig := strings.TrimPrefix(good, fmt.Sprintf("t=%d,v1=", ts))
	other := strings.Repeat("ab", 32)

	for name, tc := range map[string]struct {
		header    string
		tolerance time.Duration
	}{
		"at the edge of the tolerance":  {sign(webhookSecret, ts-300, webhookBody), 0},
		"slightly ahead":                {sign(webhookSecret, ts+30, webhookBody), 0},
		"wider tolerance":               {sign(webhookSecret, ts-3000, webhookBody), time.Hour},
		"spaces":                        {fmt.Sprintf("t=%d, v1=%s", ts, sig), 0},
		"upper-case hex":                {fmt.Sprintf("t=%d,v1=%s", ts, strings.ToUpper(sig)), 0},
		"a second signature (rotation)": {fmt.Sprintf("t=%d,v1=%s,v1=%s", ts, other, sig), 0},
		"an unknown part":               {fmt.Sprintf("t=%d,v1=%s,v2=future", ts, sig), 0},
	} {
		if _, err := VerifyWebhookAt(webhookBody, tc.header, webhookSecret, tc.tolerance, now); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVerifyWebhookPayload(t *testing.T) {
	now := time.Unix(1_800_000_100, 0)
	for _, body := range []string{"not json", `{"job":{}}`, `[]`, ``} {
		header := sign(webhookSecret, now.Unix(), []byte(body))
		if _, err := VerifyWebhookAt([]byte(body), header, webhookSecret, 0, now); !errors.Is(err, ErrWebhookPayload) {
			t.Errorf("body %q: err = %v, want ErrWebhookPayload", body, err)
		}
	}
	// An event of a kind this library does not know yet still verifies.
	body := []byte(`{"type":"job.something_new","data":{"x":1}}`)
	event, err := VerifyWebhookAt(body, sign(webhookSecret, now.Unix(), body), webhookSecret, 0, now)
	if err != nil || event.Type != "job.something_new" || event.Job != nil || string(event.Raw) != string(body) {
		t.Errorf("event = %+v, err = %v", event, err)
	}
}

func TestWebhookErrorsAreDistinct(t *testing.T) {
	all := []error{ErrWebhookHeader, ErrWebhookSignature, ErrWebhookTimestamp, ErrWebhookPayload}
	for i, a := range all {
		for j, b := range all {
			if (i == j) != errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = %v", a, b, errors.Is(a, b))
			}
		}
		if !strings.HasPrefix(a.Error(), "photos: webhook: ") {
			t.Errorf("message = %q", a.Error())
		}
		var apiErr *Error
		if errors.As(a, &apiErr) {
			t.Errorf("%v is an API error", a)
		}
	}
}
