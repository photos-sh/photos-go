package photos

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestRunReadsBarcodesAndText(t *testing.T) {
	headers := map[string]string{
		"X-Image-Width": "2000",
		"X-Barcodes":    `[{"format":"EAN-13","value":"3850104010017","gs1":"3850104010017"},{"format":"QR","value":"https://x.example","gs1":null}]`,
		// The API escapes everything outside ASCII, so the header is valid.
		"X-Text": `["Pr\u0161ut","500 g"]`,
	}
	f := imageFake(t, "image/jpeg", headers)
	img, err := f.client.Run(context.Background(), Bytes("a.jpg", []byte("photo")),
		&RunOptions{Recipe: "[barcodes]\n[text]\n"})
	if err != nil {
		t.Fatal(err)
	}
	gs1 := "3850104010017"
	want := []Barcode{
		{Format: "EAN-13", Value: "3850104010017", GS1: &gs1},
		{Format: "QR", Value: "https://x.example"},
	}
	if !reflect.DeepEqual(img.Barcodes, want) {
		t.Errorf("barcodes = %+v", img.Barcodes)
	}
	if !reflect.DeepEqual(img.Text, []string{"Pršut", "500 g"}) || img.TextTruncated {
		t.Errorf("text = %q, truncated %v", img.Text, img.TextTruncated)
	}
	if img.Width != 2000 {
		t.Errorf("width = %d", img.Width)
	}
}

func TestReadingsAbsentEmptyAndUnreadable(t *testing.T) {
	ctx := context.Background()
	run := func(headers map[string]string) *Image {
		t.Helper()
		f := imageFake(t, "image/png", headers)
		img, err := f.client.Run(ctx, Bytes("a.png", []byte("photo")), &RunOptions{Preset: "web"})
		if err != nil {
			t.Fatal(err)
		}
		return img
	}

	// No reading step: nil, not empty.
	if img := run(nil); img.Barcodes != nil || img.Text != nil || img.TextTruncated {
		t.Errorf("without reading steps = %+v, %+v", img.Barcodes, img.Text)
	}
	// The steps ran and found nothing: empty, not nil.
	img := run(map[string]string{"X-Barcodes": "[]", "X-Text": "[]"})
	if img.Barcodes == nil || len(img.Barcodes) != 0 || img.Text == nil || len(img.Text) != 0 {
		t.Errorf("found nothing = %#v, %#v", img.Barcodes, img.Text)
	}
	// Cut short.
	if img := run(map[string]string{"X-Text": `["a"]`, "X-Text-Truncated": "true"}); !img.TextTruncated {
		t.Error("TextTruncated = false")
	}
	// Not the JSON expected: read as absent, and the image is still there.
	img = run(map[string]string{"X-Barcodes": "{nope", "X-Text": `{"a":1}`})
	if img.Barcodes != nil || img.Text != nil || string(img.Bytes) != "encoded image" {
		t.Errorf("unreadable = %+v, %+v", img.Barcodes, img.Text)
	}
}

func TestJobItemKeepsReadings(t *testing.T) {
	res := jobItem(t, "image/webp", map[string]string{
		"x-image-width": "800",
		"x-barcodes":    `[{"format":"EAN-13","value":"3850104010017","gs1":"3850104010017"}]`,
		"x-text":        `["SALE"]`,
	})
	img := res.Image()
	if img == nil || len(img.Barcodes) != 1 || img.Barcodes[0].Value != "3850104010017" {
		t.Fatalf("image = %+v", img)
	}
	if !reflect.DeepEqual(img.Text, []string{"SALE"}) {
		t.Errorf("text = %q", img.Text)
	}
}

func TestPreset(t *testing.T) {
	const toml = "description = \"Web\"\n[output]\nformat = \"webp\"\n"
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		if r.Path != "/api/v1/presets/web" {
			writeError(w, 404, "not_found", `no preset named "nope"`)
			return
		}
		w.Header().Set("Content-Type", "application/toml; charset=utf-8")
		w.Write([]byte(toml))
	})
	ctx := context.Background()

	got, err := f.client.Preset(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	if got != toml {
		t.Errorf("preset = %q", got)
	}
	if r := f.last(); r.Method != "GET" {
		t.Errorf("method = %s", r.Method)
	}

	_, err = f.client.Preset(ctx, "nope")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || apiErr.Code != "not_found" {
		t.Errorf("unknown preset: %v", err)
	}

	// A name is one path segment, whatever it holds.
	_, _ = f.client.Preset(ctx, "a/b")
	if r := f.last(); r.Path != "/api/v1/presets/a/b" && r.Path != "/api/v1/presets/a%2Fb" {
		t.Errorf("path = %q", r.Path)
	}

	if _, err := f.client.Preset(ctx, ""); err == nil {
		t.Error("an empty name was sent")
	}
}
