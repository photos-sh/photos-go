package photos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Operations a job can run, for JobRequest.Operation.
const (
	OperationRun              = "run"
	OperationRemoveBackground = "remove-background"
	OperationResize           = "resize"
	OperationUpscale          = "upscale"
	OperationReadText         = "ocr"
	OperationReadBarcodes     = "barcodes"
	OperationCheck            = "check"
	OperationFix              = "fix"
	OperationRemoveText       = "remove-text"
	OperationReadLabel        = "label"
	OperationEnhance          = "enhance"
	OperationCrop             = "crop"
	OperationPalette          = "palette"
	OperationWatermark        = "watermark" // with text only: an item is one image
	OperationExpand           = "expand"
	OperationVectorize        = "vectorize"
	OperationFindFaces        = "faces"
	OperationBlurFaces        = "blur-faces"
	OperationIDPhoto          = "id-photo"
	OperationDenoise          = "denoise"
	OperationColorize         = "colorize"
	OperationSelect           = "select"
	OperationRestoreFaces     = "restore-faces"
	OperationCaption          = "caption"
)

// Job statuses, in Job.Status.
const (
	JobQueued  = "queued"
	JobRunning = "running"
	// JobCompleted means every item was attempted; Job.Counts says how many
	// failed.
	JobCompleted = "completed"
)

// Item statuses, in JobItem.Status.
const (
	ItemPending = "pending"
	ItemDone    = "done"
	ItemFailed  = "failed"
)

// Defaults of Jobs.Wait.
const (
	DefaultWaitInterval = 2 * time.Second
	DefaultWaitTimeout  = 10 * time.Minute
)

// JobsService runs one operation on many images in the background. Use it
// through Client.Jobs.
type JobsService struct {
	client *Client
}

// JobRequest describes a new job: one operation applied to every input.
type JobRequest struct {
	// Operation is the operation to run: one of the Operation constants.
	Operation string
	// Params are the operation's parameters, named as in the HTTP API:
	// {"format": "png", "crop": true}. Values may be strings, booleans and
	// numbers, and a []string for a parameter that repeats, such as the
	// "point" of a "select" job.
	//
	// An item is one image, so a "watermark" job takes "text" only and a
	// "remove-background" job no background image.
	Params map[string]any
	// Recipe is a recipe as TOML text, for OperationRun.
	Recipe string
	// URLs are images for the API to fetch: http(s) on public addresses.
	URLs []string
	// Images are images to upload. With none, the job is sent as JSON; with
	// any, as multipart/form-data, streamed like any other upload.
	Images []Input
	// WebhookURL, if set, receives a signed "job.completed" event when the
	// job is done; see VerifyWebhook.
	WebhookURL string
}

// Job is a batch job: its status, progress and per-item results.
type Job struct {
	ID        string `json:"id"`
	Operation string `json:"operation"`
	// Params is the operation's parameters as a query string.
	Params string `json:"params"`
	// Status is JobQueued, JobRunning or JobCompleted.
	Status string `json:"status"`
	// CreatedAt and FinishedAt are Unix seconds; FinishedAt is zero until
	// the job has completed.
	CreatedAt  int64     `json:"created_at"`
	FinishedAt int64     `json:"finished_at,omitempty"`
	Counts     JobCounts `json:"counts"`
	Items      []JobItem `json:"items"`
	// Webhook is the state of the webhook delivery, when the job has one.
	Webhook *JobWebhook `json:"webhook,omitempty"`
	// Raw is the job as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the job and keeps the JSON in Raw.
func (j *Job) UnmarshalJSON(data []byte) (err error) {
	type plain Job
	j.Raw, err = keepRaw(data, (*plain)(j))
	return err
}

// Completed reports whether every item was attempted.
func (j *Job) Completed() bool { return j.Status == JobCompleted }

// JobCounts counts a job's items by status.
type JobCounts struct {
	Total   int `json:"total"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
	Pending int `json:"pending"`
}

// JobItem is one input of a job and what became of it.
type JobItem struct {
	// Input is the upload's file name, or the input URL.
	Input string `json:"input"`
	// URL is the input URL, for an input that was one.
	URL string `json:"url,omitempty"`
	// Status is ItemPending, ItemDone or ItemFailed.
	Status string `json:"status"`
	// Result is the path of the result when done; fetch it with Jobs.Item.
	Result      string `json:"result,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	// Bytes is the size of the result.
	Bytes int64 `json:"bytes,omitempty"`
	// Headers are the result's headers, in lower case: x-image-width,
	// x-image-height, x-image-quality and server-timing, plus
	// x-text-removed and x-text-kept for a "remove-text" job,
	// x-marketplace-passed and x-marketplace-findings for a "fix" job, and
	// likewise x-subject-found, x-paths, x-faces, x-faces-skipped,
	// x-select-score and x-id-photo-* for the operations that report them.
	// (Older engines keep only the first four.)
	// Jobs.Item returns the same headers with the result, and its
	// RemoveText, Fix, RemoveBackground, Crop, Vectorize, BlurFaces,
	// IDPhoto, Select and RestoreFaces read them for you.
	Headers map[string]string `json:"headers,omitempty"`
	// Error says why a failed item failed: an error code of the API, or
	// CodeInputUnavailable when the input URL could not be fetched.
	Error *ItemError `json:"error,omitempty"`
}

// ItemError is why a job item failed: the error the synchronous endpoint
// would have answered with.
type ItemError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// UnmarshalJSON accepts the usual {"code", "message"} object, and a bare
// string as the message.
func (e *ItemError) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) == nil {
		*e = ItemError{Message: s}
		return nil
	}
	type plain ItemError
	return json.Unmarshal(data, (*plain)(e))
}

func (e *ItemError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// JobWebhook is the state of a job's webhook delivery.
type JobWebhook struct {
	URL       string `json:"url"`
	Attempts  int    `json:"attempts"`
	Delivered bool   `json:"delivered"`
	LastError string `json:"last_error,omitempty"`
}

// JobSummary is a job as Jobs.List shows it: without its items.
type JobSummary struct {
	ID        string `json:"id"`
	Operation string `json:"operation"`
	Status    string `json:"status"`
	// CreatedAt and FinishedAt are Unix seconds; FinishedAt is zero until
	// the job has completed.
	CreatedAt  int64     `json:"created_at"`
	FinishedAt int64     `json:"finished_at"`
	Counts     JobCounts `json:"counts"`
}

// JobItemResult is the result of one finished item: what the synchronous
// endpoint would have returned.
type JobItemResult struct {
	// Body is the raw result: an encoded image, or JSON.
	Body []byte
	// ContentType is its media type.
	ContentType string
	// Header is every header of the response.
	Header http.Header

	status int
}

// IsImage reports whether the result is an image.
func (r *JobItemResult) IsImage() bool {
	return strings.HasPrefix(mediaType(r.ContentType), "image/")
}

// IsJSON reports whether the result is JSON.
func (r *JobItemResult) IsJSON() bool {
	t := mediaType(r.ContentType)
	return t == "application/json" || strings.HasSuffix(t, "+json")
}

// Image returns the result as an Image, with its size, quality, timing and
// SubjectFound (a "crop" or "remove-background" job) read from the headers,
// or nil if the result is not an image. The SVG of a "vectorize" job is one.
func (r *JobItemResult) Image() *Image {
	if !r.IsImage() {
		return nil
	}
	return newImage(r.response())
}

func (r *JobItemResult) response() *response {
	return &response{status: r.status, header: r.Header, body: r.Body}
}

// need reports, as an ErrMissingResultHeaders, that the result is not an
// image carrying header. An empty header asks for an image only.
func (r *JobItemResult) need(method, operation, header string) error {
	if !r.IsImage() {
		return fmt.Errorf("%w %s reads: the result is %s, not the image of a %q job",
			ErrMissingResultHeaders, method, r.ContentType, operation)
	}
	if header != "" && r.Header.Get(header) == "" {
		return fmt.Errorf("%w %s reads: no %s (not a %q job, or an engine that does not keep it)",
			ErrMissingResultHeaders, method, header, operation)
	}
	return nil
}

// RemoveText returns the result of a "remove-text" job's item as the
// synchronous Client.RemoveText would: the image, and how many lines of text
// were removed and kept.
//
// If the result does not carry the X-Text-Removed header (it belongs to
// another operation, or the engine is too old to keep the header on job
// items), RemoveText returns no result and an error for which
// errors.Is(err, ErrMissingResultHeaders) is true, rather than zero counts.
// Image still gives the image.
func (r *JobItemResult) RemoveText() (*RemoveTextResult, error) {
	if err := r.need("RemoveText", OperationRemoveText, headerTextRemoved); err != nil {
		return nil, err
	}
	return newRemoveTextResult(r.response()), nil
}

// Fix returns the result of a "fix" job's item as the synchronous Client.Fix
// would: the image, whether it passes the marketplace's check, and the
// findings still present.
//
// If the result does not carry the X-Marketplace-Passed header (it belongs
// to another operation, or the engine is too old to keep the header on job
// items), Fix returns no result and an error for which
// errors.Is(err, ErrMissingResultHeaders) is true, rather than a false
// verdict. Image still gives the image.
func (r *JobItemResult) Fix() (*FixResult, error) {
	if err := r.need("Fix", OperationFix, headerMarketplacePassed); err != nil {
		return nil, err
	}
	return newFixResult(r.response()), nil
}

// RemoveBackground returns the result of a "remove-background" job's item as
// the synchronous Client.RemoveBackground would: the image, whose
// SubjectFound is false when a canvas was asked for and the photo has no
// subject.
//
// The API sends the X-Subject-Found header only then. A result without it is
// not an error: its SubjectFound is nil, which means "not known". A result
// that is not an image is an ErrMissingResultHeaders.
func (r *JobItemResult) RemoveBackground() (*Image, error) {
	if err := r.need("RemoveBackground", OperationRemoveBackground, ""); err != nil {
		return nil, err
	}
	return newImage(r.response()), nil
}

// Crop returns the result of a "crop" job's item as the synchronous
// Client.Crop would: the image, whose SubjectFound says whether a subject
// was found. Without the X-Subject-Found header, which a crop always
// carries, it returns an ErrMissingResultHeaders, as RemoveText does.
func (r *JobItemResult) Crop() (*Image, error) {
	if err := r.need("Crop", OperationCrop, headerSubjectFound); err != nil {
		return nil, err
	}
	return newImage(r.response()), nil
}

// Vectorize returns the result of a "vectorize" job's item as the
// synchronous Client.Vectorize would: the SVG and how many paths it has.
// Without the X-Paths header it returns an ErrMissingResultHeaders, as
// RemoveText does.
func (r *JobItemResult) Vectorize() (*VectorizeResult, error) {
	if err := r.need("Vectorize", OperationVectorize, headerPaths); err != nil {
		return nil, err
	}
	return newVectorizeResult(r.response()), nil
}

// BlurFaces returns the result of a "blur-faces" job's item as the
// synchronous Client.BlurFaces would: the image and how many faces were
// hidden. Without the X-Faces header it returns an ErrMissingResultHeaders,
// as RemoveText does.
func (r *JobItemResult) BlurFaces() (*BlurFacesResult, error) {
	if err := r.need("BlurFaces", OperationBlurFaces, headerFaces); err != nil {
		return nil, err
	}
	return newBlurFacesResult(r.response()), nil
}

// IDPhoto returns the result of an "id-photo" job's item as the synchronous
// Client.IDPhoto would: the photo, the findings and the head's size.
// Without the X-Id-Photo-Passed header it returns an
// ErrMissingResultHeaders, as RemoveText does.
func (r *JobItemResult) IDPhoto() (*IDPhotoResult, error) {
	if err := r.need("IDPhoto", OperationIDPhoto, headerIDPhotoPassed); err != nil {
		return nil, err
	}
	return newIDPhotoResult(r.response()), nil
}

// Select returns the result of a "select" job's item as the synchronous
// Client.Select would: the cutout or the mask, and the model's score.
// Without the X-Select-Score header it returns an ErrMissingResultHeaders,
// as RemoveText does.
func (r *JobItemResult) Select() (*SelectResult, error) {
	if err := r.need("Select", OperationSelect, headerSelectScore); err != nil {
		return nil, err
	}
	return newSelectResult(r.response()), nil
}

// RestoreFaces returns the result of a "restore-faces" job's item as the
// synchronous Client.RestoreFaces would: the image, and how many faces were
// restored and skipped. Without the X-Faces-Skipped header it returns an
// ErrMissingResultHeaders, as RemoveText does.
func (r *JobItemResult) RestoreFaces() (*RestoreFacesResult, error) {
	if err := r.need("RestoreFaces", OperationRestoreFaces, headerFacesSkipped); err != nil {
		return nil, err
	}
	return newRestoreFacesResult(r.response()), nil
}

// JSON decodes a JSON result into v: a *CheckResult for a "check" job, a
// *TextResult for "ocr", a *BarcodesResult for "barcodes", a *LabelResult
// for "label", a *PaletteResult for "palette", a *FacesResult for "faces",
// a *CaptionResult for "caption", or any type of your own.
func (r *JobItemResult) JSON(v any) error {
	if !r.IsJSON() {
		return fmt.Errorf("photos: the result is %s, not JSON", r.ContentType)
	}
	if err := json.Unmarshal(r.Body, v); err != nil {
		return fmt.Errorf("photos: decoding the result: %w", err)
	}
	return nil
}

// WaitOptions say how Jobs.Wait polls. Zero fields mean the defaults.
type WaitOptions struct {
	// Interval is the time between polls (default DefaultWaitInterval, 2 s).
	Interval time.Duration
	// Timeout is how long to wait in all (default DefaultWaitTimeout,
	// 10 min).
	Timeout time.Duration
}

// Create starts a job and returns at once; follow it with Get or Wait, or
// give a WebhookURL.
//
// Unlike other calls, Create is sent again only after a 503: after a
// connection failure the job may already exist.
func (s *JobsService) Create(ctx context.Context, req *JobRequest) (*Job, error) {
	if req == nil {
		return nil, errors.New("photos: Jobs.Create: nil request")
	}
	if req.Operation == "" {
		return nil, errors.New("photos: Jobs.Create: no operation")
	}
	if len(req.URLs) == 0 && len(req.Images) == 0 {
		return nil, errors.New("photos: Jobs.Create: no inputs: give URLs or Images")
	}

	var (
		body *requestBody
		err  error
	)
	if len(req.Images) == 0 {
		payload := map[string]any{"operation": req.Operation, "inputs": req.URLs}
		if repeatsParam(req.Params) {
			// An object cannot say a parameter twice; a query string can.
			payload["params"] = encodeParams(req.Params)
		} else if len(req.Params) > 0 {
			payload["params"] = req.Params
		}
		if req.Recipe != "" {
			payload["recipe"] = req.Recipe
		}
		if req.WebhookURL != "" {
			payload["webhook_url"] = req.WebhookURL
		}
		body, err = jsonBody(payload)
	} else {
		parts := []formPart{textPart("operation", req.Operation)}
		if len(req.Params) > 0 {
			parts = append(parts, textPart("params", encodeParams(req.Params)))
		}
		if req.Recipe != "" {
			parts = append(parts, recipePart(req.Recipe))
		}
		if req.WebhookURL != "" {
			parts = append(parts, textPart("webhook_url", req.WebhookURL))
		}
		for _, u := range req.URLs {
			parts = append(parts, textPart("url", u))
		}
		for _, in := range req.Images {
			parts = append(parts, filePart("image", in))
		}
		body, err = multipartBody(parts)
	}
	if err != nil {
		return nil, err
	}

	resp, err := s.client.send(ctx, &request{method: http.MethodPost, path: "/v1/jobs", body: body})
	if err != nil {
		return nil, err
	}
	job := &Job{}
	return job, decodeJSON(resp, job)
}

// repeatsParam reports whether a parameter has a list of values.
func repeatsParam(params map[string]any) bool {
	for _, v := range params {
		switch v.(type) {
		case []string, []any:
			return true
		}
	}
	return false
}

// encodeParams turns params into a query string, keys in order.
func encodeParams(params map[string]any) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	add := func(k, v string) {
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(v))
	}
	for _, k := range keys {
		switch v := params[k].(type) {
		case nil:
		case []string:
			for _, item := range v {
				add(k, item)
			}
		case []any:
			for _, item := range v {
				add(k, paramString(item))
			}
		default:
			add(k, paramString(v))
		}
	}
	return b.String()
}

func paramString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	default:
		return fmt.Sprint(v)
	}
}

func jobPath(id string) (string, error) {
	if id == "" {
		return "", errors.New("photos: empty job id")
	}
	return "/v1/jobs/" + url.PathEscape(id), nil
}

// Get returns a job's status, progress and per-item results.
func (s *JobsService) Get(ctx context.Context, id string) (*Job, error) {
	path, err := jobPath(id)
	if err != nil {
		return nil, err
	}
	job := &Job{}
	if err := s.client.getJSON(ctx, path, job); err != nil {
		return nil, err
	}
	return job, nil
}

// List returns your recent jobs, newest first (up to 100), without their
// items.
func (s *JobsService) List(ctx context.Context) ([]JobSummary, error) {
	var out []JobSummary
	if err := s.client.getJSON(ctx, "/v1/jobs", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete cancels a job if it is unfinished, and deletes it and its results.
func (s *JobsService) Delete(ctx context.Context, id string) error {
	path, err := jobPath(id)
	if err != nil {
		return err
	}
	_, err = s.client.send(ctx, &request{method: http.MethodDelete, path: path, retryConnection: true})
	return err
}

// Item returns the result of item n (from 0) of a job, once that item is
// done.
func (s *JobsService) Item(ctx context.Context, id string, n int) (*JobItemResult, error) {
	path, err := jobPath(id)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, errors.New("photos: negative item number")
	}
	resp, err := s.client.send(ctx, &request{
		method: http.MethodGet, path: path + "/items/" + strconv.Itoa(n), retryConnection: true,
	})
	if err != nil {
		return nil, err
	}
	return &JobItemResult{
		Body:        resp.body,
		ContentType: resp.header.Get("Content-Type"),
		Header:      resp.header,
		status:      resp.status,
	}, nil
}

// Wait polls a job until it has completed and returns it. opts may be nil.
//
// If the job has not completed within opts.Timeout, Wait returns the job as
// last seen and an error for which errors.Is(err, ErrWaitTimeout) is true.
// If ctx ends first, it returns ctx's error.
func (s *JobsService) Wait(ctx context.Context, id string, opts *WaitOptions) (*Job, error) {
	interval, timeout := DefaultWaitInterval, DefaultWaitTimeout
	if opts != nil {
		if opts.Interval > 0 {
			interval = opts.Interval
		}
		if opts.Timeout > 0 {
			timeout = opts.Timeout
		}
	}
	c := s.client
	deadline := c.now().Add(timeout)
	for {
		job, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if job.Completed() {
			return job, nil
		}
		if !c.now().Add(interval).Before(deadline) {
			return job, fmt.Errorf("%w %s: still %s after %s", ErrWaitTimeout, id, job.Status, timeout)
		}
		if err := c.sleep(ctx, interval); err != nil {
			return job, err
		}
	}
}
