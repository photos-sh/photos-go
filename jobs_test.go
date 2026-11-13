package photos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const jobJSON = `{"id":"job_abc","operation":"remove-background","params":"format=png","status":"running",
  "created_at":1800000000,
  "items":[
    {"input":"a.jpg","status":"done","result":"/api/v1/jobs/job_abc/items/0","content_type":"image/png","bytes":1234,
     "headers":{"x-image-width":"800","x-image-height":"600","server-timing":"model;dur=400"}},
    {"input":"https://example.com/b.jpg","url":"https://example.com/b.jpg","status":"failed",
     "error":{"code":"bad_request","message":"could not read the image"}},
    {"input":"c.jpg","status":"pending"}],
  "counts":{"total":3,"done":1,"failed":1,"pending":1},
  "webhook":{"url":"https://example.com/hook","attempts":1,"delivered":false,"last_error":"HTTP 500"},
  "new_field":true}`

func completedJob() string {
	s := strings.Replace(jobJSON, `"status":"running"`, `"status":"completed"`, 1)
	return strings.Replace(s, `"created_at":1800000000,`, `"created_at":1800000000,"finished_at":1800000042,`, 1)
}

func checkJob(t *testing.T, job *Job) {
	t.Helper()
	if job.ID != "job_abc" || job.Operation != OperationRemoveBackground || job.Params != "format=png" ||
		job.Status != JobRunning || job.CreatedAt != 1800000000 || job.FinishedAt != 0 || job.Completed() {
		t.Errorf("job = %+v", job)
	}
	if job.Counts != (JobCounts{Total: 3, Done: 1, Failed: 1, Pending: 1}) {
		t.Errorf("counts = %+v", job.Counts)
	}
	if len(job.Items) != 3 {
		t.Fatalf("items = %+v", job.Items)
	}
	done, failed, pending := job.Items[0], job.Items[1], job.Items[2]
	if done.Input != "a.jpg" || done.Status != ItemDone || done.Result != "/api/v1/jobs/job_abc/items/0" ||
		done.ContentType != "image/png" || done.Bytes != 1234 || done.Headers["x-image-width"] != "800" || done.Error != nil {
		t.Errorf("done item = %+v", done)
	}
	if failed.Status != ItemFailed || failed.URL != "https://example.com/b.jpg" || failed.Error == nil ||
		failed.Error.Code != CodeBadRequest || failed.Error.Message != "could not read the image" {
		t.Errorf("failed item = %+v", failed)
	}
	if pending.Status != ItemPending || pending.Result != "" {
		t.Errorf("pending item = %+v", pending)
	}
	if w := job.Webhook; w == nil || w.URL != "https://example.com/hook" || w.Attempts != 1 || w.Delivered || w.LastError != "HTTP 500" {
		t.Errorf("webhook = %+v", w)
	}
	if !strings.Contains(string(job.Raw), `"new_field":true`) {
		t.Errorf("Raw = %s", job.Raw)
	}
}

func TestJobsCreateWithURLs(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		w.Header().Set("Location", "/api/v1/jobs/job_abc")
		writeJSON(w, 202, jobJSON)
	})
	job, err := f.client.Jobs.Create(context.Background(), &JobRequest{
		Operation:  OperationRemoveBackground,
		Params:     map[string]any{"format": "png", "crop": true, "padding": 10},
		URLs:       []string{"https://example.com/a.jpg", "https://example.com/b.jpg"},
		WebhookURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkJob(t, job)

	r := f.last()
	if r.Method != "POST" || r.Path != "/api/v1/jobs" || len(r.Query) != 0 {
		t.Errorf("request = %s %s ?%s", r.Method, r.Path, r.Query.Encode())
	}
	if got := r.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization = %q", got)
	}
	var sent map[string]any
	if err := json.Unmarshal(r.Body, &sent); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"operation":   "remove-background",
		"params":      map[string]any{"format": "png", "crop": true, "padding": float64(10)},
		"inputs":      []any{"https://example.com/a.jpg", "https://example.com/b.jpg"},
		"webhook_url": "https://example.com/hook",
	}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("body = %s", r.Body)
	}
}

func TestJobsCreateJSONOmitsWhatIsUnset(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 202, jobJSON) })
	_, err := f.client.Jobs.Create(context.Background(), &JobRequest{
		Operation: OperationRun, Recipe: "[[step]]\n", URLs: []string{"https://example.com/a.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := json.Unmarshal(f.last().Body, &sent); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"operation": "run", "recipe": "[[step]]\n", "inputs": []any{"https://example.com/a.jpg"}}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("body = %s", f.last().Body)
	}
}

func TestJobsCreateWithUploads(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 202, jobJSON) })
	job, err := f.client.Jobs.Create(context.Background(), &JobRequest{
		Operation:  OperationResize,
		Params:     map[string]any{"width": 800, "format": "webp", "enlarge": false, "quality": 82.5, "background": "#fff fff"},
		Recipe:     "recipe text",
		Images:     []Input{Bytes("one.jpg", []byte("1")), Bytes("two.jpg", []byte("2"))},
		URLs:       []string{"https://example.com/three.jpg"},
		WebhookURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "job_abc" {
		t.Errorf("job = %+v", job)
	}

	r := f.last()
	if r.Method != "POST" || r.Path != "/api/v1/jobs" || len(r.Query) != 0 {
		t.Errorf("request = %s %s ?%s", r.Method, r.Path, r.Query.Encode())
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
		t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
	}
	text := func(name string) string {
		p := r.part(name)
		if p == nil {
			t.Errorf("no %s part", name)
			return ""
		}
		if p.FileName != "" {
			t.Errorf("%s is sent as a file", name)
		}
		return string(p.Data)
	}
	if got := text("operation"); got != "resize" {
		t.Errorf("operation = %q", got)
	}
	// Params travel as a query string, keys in order.
	if got, want := text("params"), "background=%23fff+fff&enlarge=false&format=webp&quality=82.5&width=800"; got != want {
		t.Errorf("params = %q, want %q", got, want)
	}
	if p := r.part("recipe"); p == nil || string(p.Data) != "recipe text" || p.FileName != "recipe.toml" {
		t.Errorf("recipe part = %+v, want a file named recipe.toml", p)
	}
	if got := text("webhook_url"); got != "https://example.com/hook" {
		t.Errorf("webhook_url = %q", got)
	}
	if got := text("url"); got != "https://example.com/three.jpg" {
		t.Errorf("url = %q", got)
	}
	images := r.parts("image")
	if len(images) != 2 || images[0].FileName != "one.jpg" || string(images[0].Data) != "1" ||
		images[1].FileName != "two.jpg" || string(images[1].Data) != "2" {
		t.Errorf("image parts = %+v", images)
	}
	if r.ContentLength != int64(len(r.Body)) {
		t.Errorf("Content-Length = %d, body is %d bytes", r.ContentLength, len(r.Body))
	}
}

func TestJobsCreateValidation(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 202, jobJSON) })
	ctx := context.Background()
	for name, req := range map[string]*JobRequest{
		"nil":          nil,
		"no operation": {URLs: []string{"https://example.com/a.jpg"}},
		"no inputs":    {Operation: OperationResize},
		"empty image":  {Operation: OperationResize, Images: []Input{{}}},
	} {
		if _, err := f.client.Jobs.Create(ctx, req); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if f.count() != 0 {
		t.Errorf("%d requests were sent", f.count())
	}
}

func TestJobsCreateRetriesOnlyOn503(t *testing.T) {
	// A 503 is retried…
	var n atomic.Int32
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "5")
			writeError(w, 503, "busy", "busy")
			return
		}
		writeJSON(w, 202, jobJSON)
	})
	req := &JobRequest{Operation: OperationResize, URLs: []string{"https://example.com/a.jpg"}}
	if _, err := f.client.Jobs.Create(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if f.count() != 2 {
		t.Errorf("%d requests, want 2", f.count())
	}
	if string(f.requests[0].Body) != string(f.requests[1].Body) {
		t.Error("the retry did not carry the same body")
	}

	// …a connection failure is not: the job may have been made.
	var attempts atomic.Int32
	f = newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 202, jobJSON) },
		WithHTTPClient(&http.Client{Transport: failingTransport(1, &attempts)}))
	_, err := f.client.Jobs.Create(context.Background(), req)
	var ce *ConnectionError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a *ConnectionError", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("%d attempts, want 1", attempts.Load())
	}
}

func TestJobsGet(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, jobJSON) })
	job, err := f.client.Jobs.Get(context.Background(), "job_abc")
	if err != nil {
		t.Fatal(err)
	}
	checkJob(t, job)
	if r := f.last(); r.Method != "GET" || r.Path != "/api/v1/jobs/job_abc" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}

	if _, err := f.client.Jobs.Get(context.Background(), ""); err == nil {
		t.Error("empty id: no error")
	}
}

func TestJobsGetNotFound(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeError(w, 404, "not_found", "no such job")
	})
	_, err := f.client.Jobs.Get(context.Background(), "job_nope")
	var apiErr *Error
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &apiErr) || apiErr.Status != 404 || apiErr.Message != "no such job" {
		t.Errorf("err = %v", err)
	}
}

func TestJobsList(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `[
		  {"id":"job_2","operation":"resize","status":"running","created_at":1800000100,"finished_at":null,
		   "counts":{"total":2,"done":1,"failed":0,"pending":1}},
		  {"id":"job_1","operation":"ocr","status":"completed","created_at":1800000000,"finished_at":1800000050,
		   "counts":{"total":1,"done":1,"failed":0,"pending":0}}]`)
	})
	jobs, err := f.client.Jobs.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Method != "GET" || r.Path != "/api/v1/jobs" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	want := []JobSummary{
		{ID: "job_2", Operation: "resize", Status: JobRunning, CreatedAt: 1800000100, Counts: JobCounts{2, 1, 0, 1}},
		{ID: "job_1", Operation: "ocr", Status: JobCompleted, CreatedAt: 1800000000, FinishedAt: 1800000050, Counts: JobCounts{1, 1, 0, 0}},
	}
	if !reflect.DeepEqual(jobs, want) {
		t.Errorf("jobs = %+v", jobs)
	}
}

func TestJobsDelete(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { w.WriteHeader(204) })
	if err := f.client.Jobs.Delete(context.Background(), "job_abc"); err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Method != "DELETE" || r.Path != "/api/v1/jobs/job_abc" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}

	f = newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeError(w, 404, "not_found", "no such job") })
	if err := f.client.Jobs.Delete(context.Background(), "job_abc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want not found", err)
	}
}

func TestJobsItemImage(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/webp", []byte("webp bytes"), map[string]string{
			"X-Image-Width": "640", "X-Image-Height": "480", "X-Image-Quality": "80", "Server-Timing": "model;dur=1",
		})
	})
	res, err := f.client.Jobs.Item(context.Background(), "job_abc", 2)
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Method != "GET" || r.Path != "/api/v1/jobs/job_abc/items/2" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	if string(res.Body) != "webp bytes" || res.ContentType != "image/webp" || res.Header.Get("X-Image-Width") != "640" {
		t.Errorf("result = %+v", res)
	}
	if !res.IsImage() || res.IsJSON() {
		t.Errorf("IsImage, IsJSON = %v, %v", res.IsImage(), res.IsJSON())
	}
	img := res.Image()
	if img == nil || img.Width != 640 || img.Height != 480 || img.Quality != 80 || img.ServerTiming != "model;dur=1" ||
		string(img.Bytes) != "webp bytes" || img.Extension() != ".webp" {
		t.Errorf("image = %+v", img)
	}
	var v any
	if err := res.JSON(&v); err == nil {
		t.Error("JSON on an image: no error")
	}

	if _, err := f.client.Jobs.Item(context.Background(), "job_abc", -1); err == nil {
		t.Error("negative item: no error")
	}
}

func TestJobsItemRemoveText(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/jpeg", []byte("jpeg bytes"), map[string]string{
			"x-image-width": "640", "x-image-height": "480", "x-image-quality": "90",
			"x-text-removed": "3", "x-text-kept": "0", "server-timing": "ocr;dur=5",
		})
	})
	res, err := f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := res.RemoveText()
	if err != nil {
		t.Fatal(err)
	}
	if rt.Removed != 3 || rt.Kept != 0 || rt.Width != 640 || rt.Height != 480 || rt.Quality != 90 ||
		rt.ContentType != "image/jpeg" || string(rt.Bytes) != "jpeg bytes" || rt.ServerTiming != "ocr;dur=5" {
		t.Errorf("result = %+v", rt)
	}
	// Not a fix result.
	fix, err := res.Fix()
	if fix != nil || !errors.Is(err, ErrMissingResultHeaders) || !strings.Contains(err.Error(), "X-Marketplace-Passed") {
		t.Errorf("Fix on a remove-text result = %+v, %v", fix, err)
	}
}

func TestJobsItemFix(t *testing.T) {
	headers := map[string]string{
		"X-Image-Width": "2000", "X-Image-Height": "2000", "X-Image-Quality": "90",
		"X-Marketplace-Passed": "false", "X-Marketplace-Findings": "too_small,text_overlay",
	}
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/jpeg", []byte("jpeg bytes"), headers)
	})
	res, err := f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	fix, err := res.Fix()
	if err != nil {
		t.Fatal(err)
	}
	if fix.Passed || !reflect.DeepEqual(fix.Findings, []string{"too_small", "text_overlay"}) || fix.Width != 2000 ||
		string(fix.Bytes) != "jpeg bytes" {
		t.Errorf("result = %+v", fix)
	}
	if rt, err := res.RemoveText(); rt != nil || !errors.Is(err, ErrMissingResultHeaders) {
		t.Errorf("RemoveText on a fix result = %+v, %v", rt, err)
	}

	// Passed, with an empty findings header: an empty list, not nil.
	headers["X-Marketplace-Passed"], headers["X-Marketplace-Findings"] = "true", ""
	res, err = f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	fix, err = res.Fix()
	if err != nil || !fix.Passed || fix.Findings == nil || len(fix.Findings) != 0 {
		t.Errorf("result = %+v, %v", fix, err)
	}
}

func TestJobsItemWithoutTheHeaders(t *testing.T) {
	// An image from an older engine, or of another operation: no zeros.
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/png", []byte("png"), map[string]string{"X-Image-Width": "10", "X-Image-Height": "10"})
	})
	res, err := f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	if rt, err := res.RemoveText(); rt != nil || !errors.Is(err, ErrMissingResultHeaders) {
		t.Errorf("RemoveText = %+v, %v", rt, err)
	}
	if fix, err := res.Fix(); fix != nil || !errors.Is(err, ErrMissingResultHeaders) {
		t.Errorf("Fix = %+v, %v", fix, err)
	}
	if res.Image() == nil {
		t.Error("Image is nil for an image")
	}

	// A JSON result.
	f = newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		w.Header().Set("X-Marketplace-Passed", "true")
		w.Header().Set("X-Text-Removed", "1")
		writeJSON(w, 200, `{"lines":[]}`)
	})
	res, err = f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RemoveText(); !errors.Is(err, ErrMissingResultHeaders) {
		t.Errorf("RemoveText on JSON: err = %v", err)
	}
	_, err = res.Fix()
	if !errors.Is(err, ErrMissingResultHeaders) {
		t.Errorf("Fix on JSON: err = %v", err)
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		t.Error("the error is an API error")
	}
}

// jobItem fetches a job item whose result is an image with these headers.
func jobItem(t *testing.T, contentType string, headers map[string]string) *JobItemResult {
	t.Helper()
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, contentType, []byte("result bytes"), headers)
	})
	res, err := f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// typedResults calls every typed accessor that needs a header of the tools
// added after RemoveText and Fix, and returns the error of each.
func typedResults(res *JobItemResult) map[string]error {
	errs := map[string]error{}
	_, errs["Crop"] = res.Crop()
	_, errs["Vectorize"] = res.Vectorize()
	_, errs["BlurFaces"] = res.BlurFaces()
	_, errs["IDPhoto"] = res.IDPhoto()
	_, errs["Select"] = res.Select()
	_, errs["RestoreFaces"] = res.RestoreFaces()
	return errs
}

// wantOnly checks that the accessors named work and every other one reports
// the missing headers.
func wantOnly(t *testing.T, res *JobItemResult, working ...string) {
	t.Helper()
	for name, err := range typedResults(res) {
		works := false
		for _, w := range working {
			works = works || w == name
		}
		switch {
		case works && err != nil:
			t.Errorf("%s: %v", name, err)
		case !works && !errors.Is(err, ErrMissingResultHeaders):
			t.Errorf("%s: err = %v, want ErrMissingResultHeaders", name, err)
		}
	}
}

func TestJobsItemVectorize(t *testing.T) {
	res := jobItem(t, "image/svg+xml", map[string]string{
		"x-image-width": "512", "x-image-height": "384", "x-paths": "23",
	})
	v, err := res.Vectorize()
	if err != nil {
		t.Fatal(err)
	}
	if v.Paths != 23 || v.Width != 512 || v.Height != 384 || v.ContentType != "image/svg+xml" ||
		string(v.Bytes) != "result bytes" || v.Extension() != ".svg" {
		t.Errorf("result = %+v", v)
	}
	// An SVG is an image to the accessors, not JSON.
	if !res.IsImage() || res.IsJSON() || res.Image() == nil {
		t.Errorf("IsImage, IsJSON, Image = %v, %v, %v", res.IsImage(), res.IsJSON(), res.Image())
	}
	wantOnly(t, res, "Vectorize")
	if _, err := res.BlurFaces(); err == nil || !strings.Contains(err.Error(), "X-Faces") {
		t.Errorf("BlurFaces on a vectorize result: err = %v", err)
	}
}

func TestJobsItemFaces(t *testing.T) {
	// No face found is a count of zero, not a missing header.
	res := jobItem(t, "image/jpeg", map[string]string{"x-image-width": "640", "x-image-height": "480", "x-faces": "0"})
	blur, err := res.BlurFaces()
	if err != nil {
		t.Fatal(err)
	}
	if blur.Faces != 0 || blur.Width != 640 || string(blur.Bytes) != "result bytes" {
		t.Errorf("result = %+v", blur)
	}
	wantOnly(t, res, "BlurFaces")

	res = jobItem(t, "image/jpeg", map[string]string{"x-faces": "4"})
	if blur, err = res.BlurFaces(); err != nil || blur.Faces != 4 {
		t.Errorf("result = %+v, %v", blur, err)
	}

	res = jobItem(t, "image/jpeg", map[string]string{
		"x-image-width": "640", "x-image-height": "480", "x-image-quality": "90", "x-faces": "2", "x-faces-skipped": "1",
	})
	restored, err := res.RestoreFaces()
	if err != nil {
		t.Fatal(err)
	}
	if restored.Faces != 2 || restored.FacesSkipped != 1 || restored.Quality != 90 || restored.ContentType != "image/jpeg" {
		t.Errorf("result = %+v", restored)
	}
}

func TestJobsItemIDPhoto(t *testing.T) {
	res := jobItem(t, "image/jpeg", map[string]string{
		"x-image-width": "413", "x-image-height": "531",
		"x-id-photo-passed": "false", "x-id-photo-findings": "head_tilted,low_resolution", "x-id-photo-head-mm": "34.0",
	})
	id, err := res.IDPhoto()
	if err != nil {
		t.Fatal(err)
	}
	if id.Passed || !reflect.DeepEqual(id.Findings, []string{"head_tilted", "low_resolution"}) || id.HeadMm != 34 ||
		id.Width != 413 || id.Height != 531 {
		t.Errorf("result = %+v", id)
	}
	wantOnly(t, res, "IDPhoto")
	if fix, err := res.Fix(); fix != nil || !errors.Is(err, ErrMissingResultHeaders) {
		t.Errorf("Fix on an id-photo result = %+v, %v", fix, err)
	}

	// Nothing found, with an empty findings header: an empty list, not nil.
	res = jobItem(t, "image/jpeg", map[string]string{
		"x-id-photo-passed": "true", "x-id-photo-findings": "", "x-id-photo-head-mm": "33.5",
	})
	id, err = res.IDPhoto()
	if err != nil || !id.Passed || id.Findings == nil || len(id.Findings) != 0 || id.HeadMm != 33.5 {
		t.Errorf("result = %+v, %v", id, err)
	}
}

func TestJobsItemSelect(t *testing.T) {
	res := jobItem(t, "image/png", map[string]string{
		"x-image-width": "300", "x-image-height": "200", "x-select-score": "0.972",
	})
	sel, err := res.Select()
	if err != nil {
		t.Fatal(err)
	}
	if sel.Score != 0.972 || sel.Width != 300 || sel.Height != 200 || sel.ContentType != "image/png" {
		t.Errorf("result = %+v", sel)
	}
	wantOnly(t, res, "Select")
}

func TestJobsItemCrop(t *testing.T) {
	// A crop always says whether a subject was found.
	res := jobItem(t, "image/webp", map[string]string{
		"x-image-width": "800", "x-image-height": "1000", "x-image-quality": "80", "x-subject-found": "true",
	})
	img, err := res.Crop()
	if err != nil {
		t.Fatal(err)
	}
	if img.SubjectFound == nil || !*img.SubjectFound || img.Width != 800 || img.Height != 1000 || img.Quality != 80 ||
		img.ContentType != "image/webp" || string(img.Bytes) != "result bytes" {
		t.Errorf("image = %+v", img)
	}
	wantOnly(t, res, "Crop")

	res = jobItem(t, "image/png", map[string]string{"x-image-width": "800", "x-subject-found": "false"})
	if img, err = res.Crop(); err != nil || img.SubjectFound == nil || *img.SubjectFound {
		t.Errorf("image = %+v, %v", img, err)
	}

	// Without the header: an error, not an image that does not say.
	res = jobItem(t, "image/png", map[string]string{"x-image-width": "800"})
	img, err = res.Crop()
	if img != nil || !errors.Is(err, ErrMissingResultHeaders) || !strings.Contains(err.Error(), "X-Subject-Found") {
		t.Errorf("Crop without the header = %+v, %v", img, err)
	}
}

func TestJobsItemRemoveBackground(t *testing.T) {
	// The header comes only when a canvas was asked for and stayed empty.
	res := jobItem(t, "image/png", map[string]string{
		"x-image-width": "800", "x-image-height": "800", "x-subject-found": "false",
	})
	img, err := res.RemoveBackground()
	if err != nil {
		t.Fatal(err)
	}
	if img.SubjectFound == nil || *img.SubjectFound || img.Width != 800 || img.Height != 800 || string(img.Bytes) != "result bytes" {
		t.Errorf("image = %+v", img)
	}
	// Image says the same.
	if img := res.Image(); img == nil || img.SubjectFound == nil || *img.SubjectFound {
		t.Errorf("Image = %+v", img)
	}

	// Without the header it is not known, and that is not an error.
	res = jobItem(t, "image/png", map[string]string{"x-image-width": "640", "x-image-height": "480"})
	img, err = res.RemoveBackground()
	if err != nil {
		t.Fatal(err)
	}
	if img.SubjectFound != nil || img.Width != 640 || img.Height != 480 {
		t.Errorf("image = %+v", img)
	}
	if img := res.Image(); img == nil || img.SubjectFound != nil {
		t.Errorf("Image = %+v", img)
	}

	// A result that is not an image.
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		w.Header().Set("X-Subject-Found", "true")
		writeJSON(w, 200, `{"lines":[]}`)
	})
	res, err = f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	if img, err := res.RemoveBackground(); img != nil || !errors.Is(err, ErrMissingResultHeaders) {
		t.Errorf("RemoveBackground on JSON = %+v, %v", img, err)
	}
	if img, err := res.Crop(); img != nil || !errors.Is(err, ErrMissingResultHeaders) {
		t.Errorf("Crop on JSON = %+v, %v", img, err)
	}
}

func TestJobsItemWithoutTheNewHeaders(t *testing.T) {
	// An image of another operation, or from an older engine: no zeros.
	res := jobItem(t, "image/png", map[string]string{"X-Image-Width": "10", "X-Image-Height": "10"})
	wantOnly(t, res)
	if res.Image() == nil {
		t.Error("Image is nil for an image")
	}

	// A JSON result, whatever its headers say.
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		for _, h := range []string{"X-Subject-Found", "X-Paths", "X-Faces", "X-Faces-Skipped", "X-Id-Photo-Passed", "X-Select-Score"} {
			w.Header().Set(h, "1")
		}
		writeJSON(w, 200, `{"width":10,"height":10,"faces":[]}`)
	})
	res, err := f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	wantOnly(t, res)
	var faces FacesResult
	if err := res.JSON(&faces); err != nil || faces.Width != 10 || faces.Faces == nil {
		t.Errorf("JSON = %+v, %v", faces, err)
	}
}

// A parameter that repeats (the points of a "select" job) travels as a query
// string, with URLs as with uploads: a JSON object cannot say a name twice.
func TestJobsCreateRepeatedParams(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 202, jobJSON) })
	ctx := context.Background()
	params := map[string]any{
		"point": []string{"120,80", "130.5,90"}, "exclude": []string{"10,10"}, "mask": true,
	}
	const want = "exclude=10%2C10&mask=true&point=120%2C80&point=130.5%2C90"

	_, err := f.client.Jobs.Create(ctx, &JobRequest{
		Operation: OperationSelect, Params: params, URLs: []string{"https://example.com/a.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := json.Unmarshal(f.last().Body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["operation"] != "select" || sent["params"] != want {
		t.Errorf("body = %s, want params %q", f.last().Body, want)
	}

	_, err = f.client.Jobs.Create(ctx, &JobRequest{
		Operation: OperationSelect, Params: params, Images: []Input{Bytes("a.jpg", []byte("1"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p := f.last().part("params"); p == nil || string(p.Data) != want {
		t.Errorf("params part = %+v, want %q", p, want)
	}

	// Without a list, the JSON form keeps its object.
	_, err = f.client.Jobs.Create(ctx, &JobRequest{
		Operation: OperationSelect, Params: map[string]any{"point": "120,80"}, URLs: []string{"https://example.com/a.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sent = nil
	if err := json.Unmarshal(f.last().Body, &sent); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent["params"], map[string]any{"point": "120,80"}) {
		t.Errorf("body = %s", f.last().Body)
	}
}

// The operation names are the API's: the JobOperation enum of its OpenAPI
// document.
func TestOperationNames(t *testing.T) {
	for got, want := range map[string]string{
		OperationRun: "run", OperationRemoveBackground: "remove-background", OperationResize: "resize",
		OperationUpscale: "upscale", OperationReadText: "ocr", OperationReadBarcodes: "barcodes",
		OperationCheck: "check", OperationFix: "fix", OperationRemoveText: "remove-text", OperationReadLabel: "label",
		OperationEnhance: "enhance", OperationCrop: "crop", OperationPalette: "palette",
		OperationWatermark: "watermark", OperationExpand: "expand", OperationVectorize: "vectorize",
		OperationFindFaces: "faces", OperationBlurFaces: "blur-faces", OperationIDPhoto: "id-photo",
		OperationDenoise: "denoise", OperationColorize: "colorize", OperationSelect: "select",
		OperationRestoreFaces: "restore-faces", OperationCaption: "caption",
	} {
		if got != want {
			t.Errorf("operation %q, want %q", got, want)
		}
	}
}

func TestJobItemHeadersInTheJobView(t *testing.T) {
	var job Job
	err := json.Unmarshal([]byte(`{"id":"job_1","operation":"fix","params":"marketplace=etsy","status":"completed","created_at":1,
	  "items":[{"input":"a.jpg","status":"done","headers":{"x-image-width":"2000","x-marketplace-passed":"true","x-marketplace-findings":""}}],
	  "counts":{"total":1,"done":1,"failed":0,"pending":0}}`), &job)
	if err != nil {
		t.Fatal(err)
	}
	h := job.Items[0].Headers
	if h["x-marketplace-passed"] != "true" || h["x-image-width"] != "2000" {
		t.Errorf("headers = %v", h)
	}
	if _, ok := h["x-marketplace-findings"]; !ok {
		t.Errorf("an empty header is dropped: %v", h)
	}
}

func TestJobsItemJSON(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"lines":[{"text":"HELLO","score":0.9,"box":[[0,0],[1,0],[1,1],[0,1]]}]}`)
	})
	res, err := f.client.Jobs.Item(context.Background(), "job_abc", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsImage() || !res.IsJSON() || res.Image() != nil {
		t.Errorf("IsImage, IsJSON, Image = %v, %v, %v", res.IsImage(), res.IsJSON(), res.Image())
	}
	var text TextResult
	if err := res.JSON(&text); err != nil {
		t.Fatal(err)
	}
	if len(text.Lines) != 1 || text.Lines[0].Text != "HELLO" {
		t.Errorf("lines = %+v", text.Lines)
	}
}

func TestJobsWait(t *testing.T) {
	var n atomic.Int32
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		if n.Add(1) < 4 {
			writeJSON(w, 200, jobJSON)
			return
		}
		writeJSON(w, 200, completedJob())
	})
	job, err := f.client.Jobs.Wait(context.Background(), "job_abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !job.Completed() || job.FinishedAt != 1800000042 {
		t.Errorf("job = %+v", job)
	}
	if f.count() != 4 {
		t.Errorf("%d polls, want 4", f.count())
	}
	if r := f.last(); r.Method != "GET" || r.Path != "/api/v1/jobs/job_abc" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	// The default interval, between polls only.
	if got, want := f.slept(), []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
}

func TestJobsWaitAlreadyCompleted(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, completedJob()) })
	if _, err := f.client.Jobs.Wait(context.Background(), "job_abc", &WaitOptions{Interval: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if f.count() != 1 || len(f.slept()) != 0 {
		t.Errorf("%d polls, waits %v", f.count(), f.slept())
	}
}

func TestJobsWaitTimeout(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, jobJSON) })
	job, err := f.client.Jobs.Wait(context.Background(), "job_abc", &WaitOptions{Interval: 100 * time.Millisecond, Timeout: time.Second})
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("err = %v, want ErrWaitTimeout", err)
	}
	if !strings.Contains(err.Error(), "job_abc") {
		t.Errorf("message = %q", err)
	}
	if job == nil || job.Status != JobRunning {
		t.Errorf("job = %+v, want the job as last seen", job)
	}
	// Polls at 0, 0.1 … 0.9 s: never past the timeout.
	if f.count() != 10 {
		t.Errorf("%d polls, want 10", f.count())
	}
	for _, d := range f.slept() {
		if d != 100*time.Millisecond {
			t.Errorf("waited %v, want the interval", d)
		}
	}

	// The default timeout is ten minutes of two-second polls.
	f = newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, jobJSON) })
	if _, err := f.client.Jobs.Wait(context.Background(), "job_abc", nil); !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("err = %v, want ErrWaitTimeout", err)
	}
	if f.count() != 300 {
		t.Errorf("%d polls, want 300", f.count())
	}
}

func TestJobsWaitStopsOnErrorAndCancel(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeError(w, 404, "not_found", "no such job") })
	if _, err := f.client.Jobs.Wait(context.Background(), "job_abc", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want not found", err)
	}
	if f.count() != 1 {
		t.Errorf("%d polls, want 1", f.count())
	}

	f = newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, jobJSON) })
	ctx, cancel := context.WithCancel(context.Background())
	f.client.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, err := f.client.Jobs.Wait(ctx, "job_abc", nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrWaitTimeout) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestItemErrorShapes(t *testing.T) {
	var item JobItem
	if err := json.Unmarshal([]byte(`{"input":"a","status":"failed","error":"it broke"}`), &item); err != nil {
		t.Fatal(err)
	}
	if item.Error == nil || item.Error.Message != "it broke" || item.Error.Error() != "it broke" {
		t.Errorf("error = %+v", item.Error)
	}
	if err := json.Unmarshal([]byte(`{"input":"a","status":"failed","error":{"code":"internal","message":"500"}}`), &item); err != nil {
		t.Fatal(err)
	}
	if item.Error.Error() != "internal: 500" {
		t.Errorf("error = %q", item.Error.Error())
	}
}

func TestEncodeParams(t *testing.T) {
	got := encodeParams(map[string]any{
		"b": true, "a": "x y&z", "n": 3, "f": 1.5, "skip": nil, "list": []string{"1", "2"}, "i64": int64(7),
	})
	if want := "a=x+y%26z&b=true&f=1.5&i64=7&list=1&list=2&n=3"; got != want {
		t.Errorf("encodeParams = %q, want %q", got, want)
	}
}
