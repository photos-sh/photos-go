package photos

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var imageHeaders = map[string]string{
	"X-Image-Width":   "800",
	"X-Image-Height":  "600",
	"X-Image-Quality": "85",
	"Server-Timing":   "decode;dur=3.1, model;dur=412.0",
}

// imageFake answers every request with an image.
func imageFake(t *testing.T, contentType string, headers map[string]string) *fake {
	return newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, contentType, []byte("encoded image"), headers)
	})
}

// wantImageRequest checks a POST of one image as multipart/form-data.
func wantImageRequest(t *testing.T, f *fake, path, query string, sent []byte) *recordedRequest {
	t.Helper()
	r := f.last()
	if r.Method != "POST" || r.Path != "/api"+path {
		t.Errorf("request = %s %s, want POST /api%s", r.Method, r.Path, path)
	}
	wantQuery(t, r.Query, query)
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
		t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization = %q", got)
	}
	p := r.part("image")
	if p == nil {
		t.Fatal("no image part")
	}
	if !bytes.Equal(p.Data, sent) {
		t.Errorf("image part = %q, want %q", p.Data, sent)
	}
	if p.FileName == "" {
		t.Error("the image part has no file name")
	}
	return r
}

func TestRemoveBackground(t *testing.T) {
	f := imageFake(t, "image/png", map[string]string{
		"X-Image-Width": "800", "X-Image-Height": "600", "Server-Timing": "model;dur=412.0",
	})
	ctx := context.Background()
	in := []byte("jpeg bytes")

	img, err := f.client.RemoveBackground(ctx, Bytes("shoe.jpg", in), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/remove-background", "", in)
	if len(r.Parts) != 1 || r.Parts[0].FileName != "shoe.jpg" {
		t.Errorf("parts = %+v", r.Parts)
	}
	if string(img.Bytes) != "encoded image" || img.ContentType != "image/png" ||
		img.Width != 800 || img.Height != 600 || img.ServerTiming != "model;dur=412.0" {
		t.Errorf("image = %+v", img)
	}
	if img.Quality != 0 {
		t.Errorf("Quality = %d, want 0 when the header is absent", img.Quality)
	}
	if img.Extension() != ".png" {
		t.Errorf("Extension = %q", img.Extension())
	}

	_, err = f.client.RemoveBackground(ctx, Bytes("shoe.jpg", in), &RemoveBackgroundOptions{
		Format: FormatWebP, Quality: 80, Background: "#ffffff", Crop: true, Padding: 12, Model: ModelLumen, Studio: true, Mask: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/remove-background",
		"format=webp&quality=80&background=%23ffffff&crop=true&padding=12&model=lumen&studio=true&mask=true", in)

	// A model this library has no constant for is sent as given.
	if _, err = f.client.RemoveBackground(ctx, Bytes("shoe.jpg", in), &RemoveBackgroundOptions{Model: "nova"}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/remove-background", "model=nova", in)

	// Unset options are left out, not sent as zeros.
	if _, err = f.client.RemoveBackground(ctx, Bytes("shoe.jpg", in), &RemoveBackgroundOptions{Format: "jpg"}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/remove-background", "format=jpg", in)
}

func TestResize(t *testing.T) {
	f := imageFake(t, "image/jpeg", imageHeaders)
	in := []byte("png bytes")
	img, err := f.client.Resize(context.Background(), Bytes("a.png", in), &ResizeOptions{
		Width: 800, Height: 600, Fit: FitCover, Enlarge: true, Format: FormatJPEG,
		Quality: 85, MaxBytes: 200_000, Background: "white",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/resize",
		"width=800&height=600&fit=cover&enlarge=true&format=jpg&quality=85&max_bytes=200000&background=white", in)
	if img.Quality != 85 || img.Width != 800 || img.Height != 600 || img.ContentType != "image/jpeg" {
		t.Errorf("image = %+v", img)
	}
	if img.ServerTiming != imageHeaders["Server-Timing"] {
		t.Errorf("ServerTiming = %q", img.ServerTiming)
	}
	if img.Header.Get("X-Image-Width") != "800" {
		t.Error("Header does not carry the response headers")
	}
}

func TestUpscale(t *testing.T) {
	f := imageFake(t, "image/webp", imageHeaders)
	in := []byte("x")
	if _, err := f.client.Upscale(context.Background(), Bytes("a.png", in), &UpscaleOptions{Scale: 2.5, Format: FormatWebP, Quality: 90}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/upscale", "scale=2.5&format=webp&quality=90", in)

	if _, err := f.client.Upscale(context.Background(), Bytes("a.png", in), &UpscaleOptions{Scale: 2}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/upscale", "scale=2", in)
}

func TestErase(t *testing.T) {
	f := imageFake(t, "image/png", imageHeaders)
	in, mask := []byte("photo"), []byte("mask")
	_, err := f.client.Erase(context.Background(), Bytes("a.png", in), &EraseOptions{
		Rects:  []Rect{{X: 10, Y: 20, Width: 30, Height: 40}, {X: 0, Y: 0, Width: 5, Height: 6}},
		Mask:   Bytes("mask.png", mask),
		Format: FormatPNG, Quality: 70,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/erase", "format=png&quality=70&rect=10,20,30,40&rect=0,0,5,6", in)
	if got, want := r.Query["rect"], []string{"10,20,30,40", "0,0,5,6"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rect = %v, want %v in order", got, want)
	}
	m := r.part("mask")
	if m == nil || string(m.Data) != "mask" || m.FileName != "mask.png" {
		t.Errorf("mask part = %+v", m)
	}
	if len(r.Parts) != 2 {
		t.Errorf("%d parts, want image and mask", len(r.Parts))
	}

	// Rects alone: no mask part.
	if _, err := f.client.Erase(context.Background(), Bytes("a.png", in), &EraseOptions{Rects: []Rect{{1, 2, 3, 4}}}); err != nil {
		t.Fatal(err)
	}
	r = wantImageRequest(t, f, "/v1/erase", "rect=1,2,3,4", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
}

func TestRemoveText(t *testing.T) {
	f := imageFake(t, "image/jpeg", map[string]string{
		"X-Image-Width": "640", "X-Image-Height": "480", "X-Image-Quality": "90",
		"X-Text-Removed": "3", "X-Text-Kept": "2",
	})
	in := []byte("photo")
	res, err := f.client.RemoveText(context.Background(), Bytes("a.jpg", in), &RemoveTextOptions{
		Region: RegionAll, Matching: `sale|€|www\.`, Format: FormatJPEG, Quality: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/remove-text", "region=all&format=jpg&quality=90&matching=sale%7C%E2%82%AC%7Cwww%5C.", in)
	if got := r.Query.Get("matching"); got != `sale|€|www\.` {
		t.Errorf("matching = %q", got)
	}
	if res.Removed != 3 || res.Kept != 2 {
		t.Errorf("Removed, Kept = %d, %d", res.Removed, res.Kept)
	}
	// The image accessors are those of an Image.
	if res.Width != 640 || res.Quality != 90 || string(res.Bytes) != "encoded image" {
		t.Errorf("image = %+v", res.Image)
	}
	path := filepath.Join(t.TempDir(), "out.jpg")
	if err := res.Save(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "encoded image" {
		t.Errorf("saved %q", got)
	}
}

func TestRun(t *testing.T) {
	f := imageFake(t, "image/jpeg", imageHeaders)
	in := []byte("photo")
	ctx := context.Background()

	if _, err := f.client.Run(ctx, Bytes("a.jpg", in), &RunOptions{Preset: "marketplace"}); err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/run", "preset=marketplace", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}

	recipe := "[[step]]\nop = \"resize\"\nwidth = 100\n"
	if _, err := f.client.Run(ctx, Bytes("a.jpg", in), &RunOptions{Recipe: recipe}); err != nil {
		t.Fatal(err)
	}
	r = wantImageRequest(t, f, "/v1/run", "", in)
	p := r.part("recipe")
	if p == nil || string(p.Data) != recipe {
		t.Errorf("recipe part = %+v", p)
	}
	if p != nil && p.FileName != "recipe.toml" {
		t.Errorf("recipe file name = %q, want recipe.toml", p.FileName)
	}
	if len(r.Parts) != 2 {
		t.Errorf("%d parts, want image and recipe", len(r.Parts))
	}
}

func TestFix(t *testing.T) {
	in := []byte("photo")
	ctx := context.Background()

	f := imageFake(t, "image/jpeg", map[string]string{
		"X-Image-Width": "2000", "X-Image-Height": "2000", "X-Image-Quality": "90",
		"X-Marketplace-Passed": "false", "X-Marketplace-Findings": "too_small,text_overlay",
	})
	res, err := f.client.Fix(ctx, Bytes("a.jpg", in), "amazon")
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/fix", "marketplace=amazon", in)
	if res.Passed || !reflect.DeepEqual(res.Findings, []string{"too_small", "text_overlay"}) {
		t.Errorf("Passed, Findings = %v, %v", res.Passed, res.Findings)
	}
	if res.Width != 2000 || res.ContentType != "image/jpeg" {
		t.Errorf("image = %+v", res.Image)
	}

	// Passed, with an empty findings header: an empty list, not nil.
	f = imageFake(t, "image/jpeg", map[string]string{"X-Marketplace-Passed": "true", "X-Marketplace-Findings": ""})
	res, err = f.client.Fix(ctx, Bytes("a.jpg", in), "ebay")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Findings == nil || len(res.Findings) != 0 {
		t.Errorf("Passed, Findings = %v, %#v", res.Passed, res.Findings)
	}

	if _, err := f.client.Fix(ctx, Bytes("a.jpg", in), ""); err == nil {
		t.Error("Fix without a marketplace: no error")
	}
}

func TestCheck(t *testing.T) {
	const body = `{
	  "metrics": {"width": 1200, "height": 900, "sharpness": 0.72, "noise": null, "brightness": 0.61,
	    "highlights_clipped": 0.01, "shadows_clipped": 0.0, "brightest": 0.99,
	    "border": {"mean": [250.5, 251, 249], "white": 0.93, "cast": null},
	    "subject": {"x": 100, "y": 80, "width": 900, "height": 700, "fill": 0.78, "area": 0.5,
	      "touches": ["left"], "background_white": 0.97}},
	  "findings": [{"code": "soft", "severity": "warning", "message": "slightly soft", "detail": "sharpness 0.42"}],
	  "text": [{"text": "SALE", "on_product": false}, {"text": "ACME", "on_product": null}],
	  "marketplace": {"marketplace": "amazon", "title": "Amazon (main image)", "passed": false,
	    "findings": [{"code": "text_overlay", "severity": "error", "message": "no text"}],
	    "not_checked": [], "notes": ["No props."], "checked": "2026-11-06", "sources": ["https://example.com"]},
	  "ms": {"decode": 4, "check": 310},
	  "future_field": {"a": 1}
	}`
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, body) })
	in := []byte("photo")
	res, err := f.client.Check(context.Background(), Bytes("a.jpg", in), &CheckOptions{
		Marketplace: "amazon", Subject: true, Text: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/check", "marketplace=amazon&subject=true&text=true", in)

	m := res.Metrics
	if m.Width != 1200 || m.Sharpness != 0.72 || m.Noise != nil || m.Border.White != 0.93 || m.Border.Cast != nil {
		t.Errorf("metrics = %+v", m)
	}
	if m.Subject == nil || m.Subject.Fill != 0.78 || !reflect.DeepEqual(m.Subject.Touches, []string{"left"}) {
		t.Errorf("subject = %+v", m.Subject)
	}
	if len(res.Findings) != 1 || res.Findings[0].Code != "soft" || res.Findings[0].Detail != "sharpness 0.42" {
		t.Errorf("findings = %+v", res.Findings)
	}
	if len(res.Text) != 2 || res.Text[0].OnProduct == nil || *res.Text[0].OnProduct || res.Text[1].OnProduct != nil {
		t.Errorf("text = %+v", res.Text)
	}
	v := res.Marketplace
	if v == nil || v.Passed || v.Findings[0].Severity != "error" || v.Checked != "2026-11-06" || len(v.Notes) != 1 {
		t.Errorf("verdict = %+v", v)
	}
	if res.Ms["check"] != 310 {
		t.Errorf("ms = %v", res.Ms)
	}
	// What the library does not know yet is still there.
	var extra struct {
		Future map[string]int `json:"future_field"`
	}
	if err := json.Unmarshal(res.Raw, &extra); err != nil || extra.Future["a"] != 1 {
		t.Errorf("Raw does not keep unknown fields: %v, %s", err, res.Raw)
	}

	// No options: nothing in the query.
	if _, err := f.client.Check(context.Background(), Bytes("a.jpg", in), nil); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/check", "", in)
}

func TestReadBarcodes(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"barcodes":[{"format":"EAN-13","value":"5000281005408","gs1":"05000281005408"},
			{"format":"QR","value":"https://example.com","gs1":null}],"variant":"upscaled"}`)
	})
	in := []byte("photo")
	res, err := f.client.ReadBarcodes(context.Background(), Bytes("a.jpg", in), &ReadBarcodesOptions{AllFormats: true, Engine: "zbar"})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/barcodes", "all_formats=true&engine=zbar", in)
	if len(res.Barcodes) != 2 || res.Variant != "upscaled" {
		t.Fatalf("result = %+v", res)
	}
	if b := res.Barcodes[0]; b.Format != "EAN-13" || b.Value != "5000281005408" || b.GS1 == nil || *b.GS1 != "05000281005408" {
		t.Errorf("first barcode = %+v", b)
	}
	if res.Barcodes[1].GS1 != nil {
		t.Errorf("second barcode has a GS1 value")
	}
	if len(res.Raw) == 0 {
		t.Error("Raw is empty")
	}
}

func TestReadText(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"lines":[{"text":"GLEN EXAMPLE","score":0.98,"box":[[10,12],[200,12],[200,40.5],[10,40]]}],"ms":{"decode":2,"ocr":120}}`)
	})
	in := []byte("photo")
	res, err := f.client.ReadText(context.Background(), Bytes("a.jpg", in))
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/ocr", "", in)
	if len(res.Lines) != 1 || res.Lines[0].Text != "GLEN EXAMPLE" || res.Lines[0].Score != 0.98 {
		t.Fatalf("lines = %+v", res.Lines)
	}
	if box := res.Lines[0].Box; len(box) != 4 || box[2][1] != 40.5 {
		t.Errorf("box = %v", box)
	}
	if res.Ms["ocr"] != 120 {
		t.Errorf("ms = %v", res.Ms)
	}
}

func TestReadLabel(t *testing.T) {
	const body = `{
	  "label": {"headline": ["GLEN EXAMPLE"], "gtin": "05000281005408",
	    "gs1": {"gtin": "05000281005408", "batch": "L123", "net_weight_kg": 1.5, "count": 6},
	    "net_quantity": {"value": 70, "unit": "cl", "ml": 700, "text": "70 cl"},
	    "alcohol": {"abv": 43.5, "text": "43.5% vol"}, "age_years": 12, "vintage": 2010,
	    "dates": [{"kind": "best_before", "date": "2027-03", "text": "BB 03/2027"}],
	    "allergens": ["milk"], "nutrition": {"per": "100g", "energy_kcal": 250, "salt": 0},
	    "materials": [{"name": "cotton", "percent": 95}], "claims": ["vegan"]},
	  "barcodes": [{"format": "EAN-13", "value": "5000281005408", "photo": 0}],
	  "text": ["GLEN EXAMPLE", "70 cl"],
	  "ms": {"read": 512}
	}`
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, body) })
	front, back := []byte("front"), []byte("back")
	res, err := f.client.ReadLabel(context.Background(), []Input{Bytes("front.jpg", front), Bytes("back.jpg", back)})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/label", "", front)
	images := r.parts("image")
	if len(images) != 2 || string(images[1].Data) != "back" || images[0].FileName != "front.jpg" || images[1].FileName != "back.jpg" {
		t.Errorf("image parts = %+v", images)
	}

	l := res.Label
	if l.GTIN != "05000281005408" || l.Headline[0] != "GLEN EXAMPLE" || l.AgeYears == nil || *l.AgeYears != 12 || *l.Vintage != 2010 {
		t.Errorf("label = %+v", l)
	}
	if l.Bottled != nil || l.Lot != "" || l.Electrical != nil {
		t.Errorf("fields that were not found are set: %+v", l)
	}
	if l.GS1 == nil || l.GS1.Batch != "L123" || *l.GS1.NetWeightKg != 1.5 || *l.GS1.Count != 6 {
		t.Errorf("gs1 = %+v", l.GS1)
	}
	if q := l.NetQuantity; q == nil || q.Value != 70 || q.Unit != "cl" || *q.Ml != 700 || q.Grams != nil {
		t.Errorf("net quantity = %+v", q)
	}
	if l.Alcohol == nil || l.Alcohol.ABV != 43.5 {
		t.Errorf("alcohol = %+v", l.Alcohol)
	}
	if n := l.Nutrition; n == nil || *n.EnergyKcal != 250 || n.Salt == nil || *n.Salt != 0 || n.Fat != nil {
		t.Errorf("nutrition = %+v", n)
	}
	if len(l.Dates) != 1 || l.Dates[0].Kind != "best_before" || *l.Materials[0].Percent != 95 || l.Claims[0] != "vegan" {
		t.Errorf("label = %+v", l)
	}
	if len(res.Barcodes) != 1 || res.Barcodes[0]["value"] != "5000281005408" || res.Barcodes[0]["photo"] != float64(0) {
		t.Errorf("barcodes = %v", res.Barcodes)
	}
	if !reflect.DeepEqual(res.Text, []string{"GLEN EXAMPLE", "70 cl"}) || res.Ms["read"] != 512 {
		t.Errorf("text, ms = %v, %v", res.Text, res.Ms)
	}
}

func TestIdentify(t *testing.T) {
	const body = `{"catalogue":"whisky",
	  "candidates":[{"product_id":"p-1","title":"Glen Example 12","score":0.91,"evidence":["barcode","age"]}],
	  "label":{"ages":[12],"abvs":[43.5],"sizes":[70],
	    "quantities":[{"value":400,"unit":"g"}],"packs":[6],"spfs":[30],
	    "strengths":[{"value":200,"unit":"mg"}],"counts":[24],"specs":[{"value":128,"unit":"gb"}]},
	  "barcodes":["5000281005408"],"text":"GLEN EXAMPLE 12","ms":{"read":400,"match":3}}`
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, body) })
	a, b, c := []byte("a"), []byte("b"), []byte("c")
	res, err := f.client.Identify(context.Background(),
		[]Input{Bytes("a.jpg", a), Bytes("b.jpg", b), Bytes("c.jpg", c)},
		&IdentifyOptions{Catalogue: "whisky", Top: 3})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/identify", "catalogue=whisky&top=3", a)
	if got := r.parts("image"); len(got) != 3 || string(got[2].Data) != "c" {
		t.Errorf("image parts = %+v", got)
	}
	if res.Catalogue != "whisky" || len(res.Candidates) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if cand := res.Candidates[0]; cand.ProductID != "p-1" || cand.Score != 0.91 || !reflect.DeepEqual(cand.Evidence, []string{"barcode", "age"}) {
		t.Errorf("candidate = %+v", cand)
	}
	if !reflect.DeepEqual(res.Label.Ages, []int{12}) || res.Label.Sizes[0] != 70 || res.Barcodes[0] != "5000281005408" || res.Text != "GLEN EXAMPLE 12" {
		t.Errorf("result = %+v", res)
	}
	l := res.Label
	if !reflect.DeepEqual(l.Quantities, []Measure{{400, "g"}}) || !reflect.DeepEqual(l.Strengths, []Measure{{200, "mg"}}) ||
		!reflect.DeepEqual(l.Specs, []Measure{{128, "gb"}}) || l.Packs[0] != 6 || l.SPFs[0] != 30 || l.Counts[0] != 24 {
		t.Errorf("label = %+v", l)
	}
}

func TestImageCountLimits(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, `{}`) })
	ctx := context.Background()
	nine := make([]Input, 9)
	for i := range nine {
		nine[i] = Bytes("x.jpg", []byte("x"))
	}
	if _, err := f.client.ReadLabel(ctx, nil); err == nil {
		t.Error("ReadLabel with no images: no error")
	}
	if _, err := f.client.ReadLabel(ctx, nine); err == nil {
		t.Error("ReadLabel with nine images: no error")
	}
	if _, err := f.client.Identify(ctx, nil, nil); err == nil {
		t.Error("Identify with no images: no error")
	}
	if _, err := f.client.Identify(ctx, nine, nil); err == nil {
		t.Error("Identify with nine images: no error")
	}
	if f.count() != 0 {
		t.Errorf("%d requests were sent", f.count())
	}
	if _, err := f.client.ReadLabel(ctx, nine[:8]); err != nil {
		t.Errorf("eight images: %v", err)
	}
}

func TestAccount(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"account":"acme","key":"key_1","credits":4200,"prices":{"remove-background":2,"resize":1}}`)
	})
	acc, err := f.client.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acc.Account != "acme" || acc.Key != "key_1" || acc.Credits == nil || *acc.Credits != 4200 || acc.Unlimited {
		t.Errorf("account = %+v", acc)
	}
	if acc.Prices["remove-background"] != 2 {
		t.Errorf("prices = %v", acc.Prices)
	}

	f = newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"account":"sdk","unlimited":true}`)
	})
	acc, err = f.client.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !acc.Unlimited || acc.Credits != nil || string(acc.Raw) != `{"account":"sdk","unlimited":true}` {
		t.Errorf("account = %+v", acc)
	}
}

func TestPresets(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `[{"name":"marketplace","description":"Pure white, square."},{"name":"web","description":"WebP."}]`)
	})
	got, err := f.client.Presets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Method != "GET" || r.Path != "/api/v1/presets" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	want := []Preset{{"marketplace", "Pure white, square."}, {"web", "WebP."}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("presets = %+v", got)
	}
}

func TestMarketplaces(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `[{"name":"amazon","title":"Amazon (main image)","checked":"2026-11-06",
		  "sources":["https://example.com/rules"],"notes":["No props."],
		  "rules":[{"check":"min_longest","px":500,"severity":"error"}],
		  "fix":{"width":2000,"height":2000,"format":"jpg","white_background":true,"max_mb":null},
		  "later":"x"}]`)
	})
	got, err := f.client.Marketplaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Method != "GET" || r.Path != "/api/v1/marketplaces" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	if len(got) != 1 {
		t.Fatalf("marketplaces = %+v", got)
	}
	m := got[0]
	if m.Name != "amazon" || m.Checked != "2026-11-06" || m.Sources[0] != "https://example.com/rules" || m.Notes[0] != "No props." {
		t.Errorf("marketplace = %+v", m)
	}
	if m.Rules[0]["check"] != "min_longest" || m.Rules[0]["px"] != float64(500) {
		t.Errorf("rules = %v", m.Rules)
	}
	if m.Fix["format"] != "jpg" || m.Fix["white_background"] != true {
		t.Errorf("fix = %v", m.Fix)
	}
	if !strings.Contains(string(m.Raw), `"later":"x"`) {
		t.Errorf("Raw = %s", m.Raw)
	}
}

func TestWebhookSecret(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"secret":"whsec_abc","header":"Photos-Signature","scheme":"t=<unix>,v1=<hex>"}`)
	})
	got, err := f.client.WebhookSecret(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Method != "GET" || r.Path != "/api/v1/webhook-secret" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	if got.Secret != "whsec_abc" || got.Header != SignatureHeader || got.Scheme == "" {
		t.Errorf("secret = %+v", got)
	}
}

func TestImageExtension(t *testing.T) {
	for ct, want := range map[string]string{
		"image/png": ".png", "image/jpeg": ".jpg", "IMAGE/WEBP; q=1": ".webp", "image/avif": ".avif",
		"image/svg+xml": ".svg", "image/svg+xml; charset=utf-8": ".svg",
		"application/json": "", "": "",
	} {
		if got := (&Image{ContentType: ct}).Extension(); got != want {
			t.Errorf("Extension(%q) = %q, want %q", ct, got, want)
		}
	}
}

func TestOptionPointers(t *testing.T) {
	i, f, b := Int(0), Float(0), Bool(false)
	if i == nil || *i != 0 || f == nil || *f != 0 || b == nil || *b {
		t.Errorf("Int(0), Float(0), Bool(false) = %v, %v, %v", i, f, b)
	}
	if *Int(7) != 7 || *Float(-2.5) != -2.5 || !*Bool(true) {
		t.Error("Int, Float or Bool does not point to its argument")
	}
}

func TestRemoveBackgroundStaging(t *testing.T) {
	f := imageFake(t, "image/png", map[string]string{
		"X-Image-Width": "800", "X-Image-Height": "800", "X-Subject-Found": "false",
	})
	ctx := context.Background()
	in, backdrop := []byte("photo"), []byte("backdrop")

	img, err := f.client.RemoveBackground(ctx, Bytes("shoe.jpg", in), &RemoveBackgroundOptions{
		Width: 800, Height: 800, Fill: 0.9, Align: "bottom", Shadow: "contact", Reflection: true,
		BackgroundImage: Bytes("studio.jpg", backdrop),
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/remove-background",
		"width=800&height=800&fill=0.9&align=bottom&shadow=contact&reflection=true", in)
	b := r.part("background")
	if b == nil || string(b.Data) != "backdrop" || b.FileName != "studio.jpg" {
		t.Errorf("background part = %+v", b)
	}
	if len(r.Parts) != 2 {
		t.Errorf("%d parts, want image and background", len(r.Parts))
	}
	// The photo had no subject: it comes back as it was.
	if img.SubjectFound == nil || *img.SubjectFound {
		t.Errorf("SubjectFound = %v, want false", img.SubjectFound)
	}
	if img.Width != 800 || img.Height != 800 {
		t.Errorf("image = %+v", img)
	}

	// The photo's own background, blurred: no second image.
	_, err = f.client.RemoveBackground(ctx, Bytes("shoe.jpg", in), &RemoveBackgroundOptions{
		Blur: 40, Shadow: "drop", Format: FormatJPEG,
	})
	if err != nil {
		t.Fatal(err)
	}
	r = wantImageRequest(t, f, "/v1/remove-background", "blur=40&shadow=drop&format=jpg", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}

	// Without the header, whether a subject was found is not known.
	f = imageFake(t, "image/png", imageHeaders)
	img, err = f.client.RemoveBackground(ctx, Bytes("shoe.jpg", in), &RemoveBackgroundOptions{Width: 800, Height: 800})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/remove-background", "width=800&height=800", in)
	if img.SubjectFound != nil {
		t.Errorf("SubjectFound = %v, want nil when the header is absent", *img.SubjectFound)
	}
}

func TestResizeTrimAndDPI(t *testing.T) {
	f := imageFake(t, "image/png", imageHeaders)
	ctx := context.Background()
	in := []byte("png bytes")

	// A tolerance of 0 is sent: it is not the default.
	_, err := f.client.Resize(ctx, Bytes("a.png", in), &ResizeOptions{Width: 600, Trim: true, TrimTolerance: Int(0), DPI: 300})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/resize", "width=600&trim=true&trim_tolerance=0&dpi=300", in)

	if _, err := f.client.Resize(ctx, Bytes("a.png", in), &ResizeOptions{Trim: true, TrimTolerance: Int(25)}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/resize", "trim=true&trim_tolerance=25", in)

	// Not set: left out.
	if _, err := f.client.Resize(ctx, Bytes("a.png", in), &ResizeOptions{Trim: true}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/resize", "trim=true", in)
}

func TestEnhance(t *testing.T) {
	f := imageFake(t, "image/jpeg", imageHeaders)
	ctx := context.Background()
	in := []byte("photo")

	// No options: the automatic corrections, nothing in the query.
	img, err := f.client.Enhance(ctx, Bytes("a.jpg", in), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/enhance", "", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if img.Width != 800 || img.Height != 600 || img.Quality != 85 || img.ContentType != "image/jpeg" || img.SubjectFound != nil {
		t.Errorf("image = %+v", img)
	}
	if _, err := f.client.Enhance(ctx, Bytes("a.jpg", in), &EnhanceOptions{}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/enhance", "", in)

	// Every option; negative settings and a false are sent.
	_, err = f.client.Enhance(ctx, Bytes("a.jpg", in), &EnhanceOptions{
		Auto: Bool(true), Levels: Bool(false), WhiteBalance: Bool(true),
		Brightness: -20, Contrast: 15.5, Saturation: -100, Temperature: -7, Sharpen: 40,
		Format: FormatJPEG, Quality: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/enhance",
		"auto=true&levels=false&white_balance=true&brightness=-20&contrast=15.5&saturation=-100&temperature=-7&sharpen=40&format=jpg&quality=90", in)

	// Auto turned off is not the same as Auto not set.
	if _, err := f.client.Enhance(ctx, Bytes("a.jpg", in), &EnhanceOptions{Auto: Bool(false)}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/enhance", "auto=false", in)

	// One setting: the others are left out, not sent as zeros.
	if _, err := f.client.Enhance(ctx, Bytes("a.jpg", in), &EnhanceOptions{Brightness: 10}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/enhance", "brightness=10", in)
}

func TestCrop(t *testing.T) {
	f := imageFake(t, "image/webp", map[string]string{
		"X-Image-Width": "800", "X-Image-Height": "1000", "X-Image-Quality": "80", "X-Subject-Found": "true",
	})
	ctx := context.Background()
	in := []byte("photo")

	// A padding of 0 is sent: it is not the default.
	img, err := f.client.Crop(ctx, Bytes("a.jpg", in), &CropOptions{
		Aspect: "4:5", Width: 800, Height: 1000, Zoom: true, Padding: Float(0), Focus: "face",
		Format: FormatWebP, Quality: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/crop",
		"aspect=4:5&width=800&height=1000&zoom=true&padding=0&focus=face&format=webp&quality=80", in)
	if got := r.Query.Get("aspect"); got != "4:5" {
		t.Errorf("aspect = %q", got)
	}
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if img.SubjectFound == nil || !*img.SubjectFound {
		t.Errorf("SubjectFound = %v, want true", img.SubjectFound)
	}
	if img.Width != 800 || img.Height != 1000 || img.Quality != 80 || img.ContentType != "image/webp" {
		t.Errorf("image = %+v", img)
	}

	if _, err := f.client.Crop(ctx, Bytes("a.jpg", in), &CropOptions{Aspect: "1.5", Zoom: true, Padding: Float(0.6)}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/crop", "aspect=1.5&zoom=true&padding=0.6", in)

	// Padding not set: left out.
	if _, err := f.client.Crop(ctx, Bytes("a.jpg", in), &CropOptions{Aspect: "1:1"}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/crop", "aspect=1:1", in)
}

func TestSubjectFound(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		header string // "-" means absent
		want   *bool
	}{
		{"true", &yes}, {"false", &no}, {"TRUE", &yes}, {" false ", &no},
		{"-", nil}, {"", nil}, {"maybe", nil},
	} {
		headers := map[string]string{"X-Image-Width": "10", "X-Image-Height": "10"}
		if c.header != "-" {
			headers["X-Subject-Found"] = c.header
		}
		f := imageFake(t, "image/png", headers)
		img, err := f.client.Crop(context.Background(), Bytes("a.png", []byte("x")), &CropOptions{Aspect: "1:1"})
		if err != nil {
			t.Fatal(err)
		}
		got := img.SubjectFound
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("X-Subject-Found %q: SubjectFound = %v, want %v", c.header, show(got), show(c.want))
		}
	}
}

func show(b *bool) string {
	if b == nil {
		return "nil"
	}
	if *b {
		return "true"
	}
	return "false"
}

func TestWatermark(t *testing.T) {
	f := imageFake(t, "image/jpeg", imageHeaders)
	ctx := context.Background()
	in, logo := []byte("photo"), []byte("logo")

	// Text; a margin of 0 is sent: it is not the default.
	img, err := f.client.Watermark(ctx, Bytes("a.jpg", in), &WatermarkOptions{
		Text: "© Acme & Co", Color: "#ffffff", Position: "bottom-left", Scale: 30, Opacity: 60,
		Margin: Float(0), Tile: true, Format: FormatJPEG, Quality: 85,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/watermark",
		"text=%C2%A9+Acme+%26+Co&color=%23ffffff&position=bottom-left&scale=30&opacity=60&margin=0&tile=true&format=jpg&quality=85", in)
	if got := r.Query.Get("text"); got != "© Acme & Co" {
		t.Errorf("text = %q", got)
	}
	if len(r.Parts) != 1 || r.part("watermark") != nil {
		t.Errorf("parts = %+v, want only the image", r.Parts)
	}
	if img.Width != 800 || img.ContentType != "image/jpeg" {
		t.Errorf("image = %+v", img)
	}

	// A logo travels as the watermark part, beside the image.
	_, err = f.client.Watermark(ctx, Bytes("a.jpg", in), &WatermarkOptions{
		Watermark: Bytes("logo.png", logo), Position: "center", Scale: 12.5, Margin: Float(4.5),
	})
	if err != nil {
		t.Fatal(err)
	}
	r = wantImageRequest(t, f, "/v1/watermark", "position=center&scale=12.5&margin=4.5", in)
	w := r.part("watermark")
	if w == nil || string(w.Data) != "logo" || w.FileName != "logo.png" {
		t.Errorf("watermark part = %+v", w)
	}
	if len(r.Parts) != 2 {
		t.Errorf("%d parts, want image and watermark", len(r.Parts))
	}

	// Margin not set: left out.
	if _, err := f.client.Watermark(ctx, Bytes("a.jpg", in), &WatermarkOptions{Text: "Acme"}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/watermark", "text=Acme", in)
}

func TestExpand(t *testing.T) {
	f := imageFake(t, "image/png", imageHeaders)
	ctx := context.Background()
	in := []byte("photo")

	img, err := f.client.Expand(ctx, Bytes("a.png", in), &ExpandOptions{Aspect: "16:9", Fill: "inpaint"})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/expand", "aspect=16:9&fill=inpaint", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if img.Width != 800 || img.Height != 600 {
		t.Errorf("image = %+v", img)
	}

	// By pixels; a side that does not grow is left out.
	_, err = f.client.Expand(ctx, Bytes("a.png", in), &ExpandOptions{
		Left: 100, Right: 120, Top: 20, Fill: "#ffffff", Format: FormatPNG, Quality: 70,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/expand", "left=100&right=120&top=20&fill=%23ffffff&format=png&quality=70", in)

	if _, err := f.client.Expand(ctx, Bytes("a.png", in), &ExpandOptions{Bottom: 64}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/expand", "bottom=64", in)
}

func TestVectorize(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512"><path d="M0 0h8v8z"/></svg>`
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeImage(w, "image/svg+xml", []byte(svg), map[string]string{
			"X-Image-Width": "512", "X-Image-Height": "512", "X-Paths": "23",
		})
	})
	ctx := context.Background()
	in := []byte("logo")

	// A speckle of 0 is sent: it is not the default.
	res, err := f.client.Vectorize(ctx, Bytes("logo.png", in), &VectorizeOptions{
		Mode: "mono", Curves: "polygon", Detail: 6, Speckle: Int(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/vectorize", "mode=mono&curves=polygon&detail=6&speckle=0", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if res.Paths != 23 {
		t.Errorf("Paths = %d", res.Paths)
	}
	// The result is an Image whose bytes are SVG text.
	if string(res.Bytes) != svg || res.ContentType != "image/svg+xml" || res.Width != 512 || res.Height != 512 ||
		res.Quality != 0 || res.Extension() != ".svg" {
		t.Errorf("image = %+v", res.Image)
	}
	path := filepath.Join(t.TempDir(), "logo"+res.Extension())
	if err := res.Save(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != svg {
		t.Errorf("saved %q", got)
	}

	// No options: nothing in the query.
	if _, err := f.client.Vectorize(ctx, Bytes("logo.png", in), nil); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/vectorize", "", in)
	if _, err := f.client.Vectorize(ctx, Bytes("logo.png", in), &VectorizeOptions{Speckle: Int(12)}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/vectorize", "speckle=12", in)

	// Without the header: zero.
	f = imageFake(t, "image/svg+xml", nil)
	res, err = f.client.Vectorize(ctx, Bytes("logo.png", in), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Paths != 0 || res.Width != 0 {
		t.Errorf("Paths, Width = %d, %d, want zeros when the headers are absent", res.Paths, res.Width)
	}
}

func TestBlurFaces(t *testing.T) {
	f := imageFake(t, "image/jpeg", map[string]string{
		"X-Image-Width": "640", "X-Image-Height": "480", "X-Image-Quality": "88", "X-Faces": "3",
	})
	ctx := context.Background()
	in := []byte("photo")

	// A padding of 0 is sent: it is not the default.
	res, err := f.client.BlurFaces(ctx, Bytes("a.jpg", in), &BlurFacesOptions{
		Mode: "fill", Strength: 70, Padding: Float(0), Color: "#000000", MinScore: 0.4,
		Format: FormatJPEG, Quality: 88,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/blur-faces",
		"mode=fill&strength=70&padding=0&color=%23000000&min_score=0.4&format=jpg&quality=88", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if res.Faces != 3 {
		t.Errorf("Faces = %d", res.Faces)
	}
	if res.Width != 640 || res.Quality != 88 || string(res.Bytes) != "encoded image" {
		t.Errorf("image = %+v", res.Image)
	}

	if _, err := f.client.BlurFaces(ctx, Bytes("a.jpg", in), &BlurFacesOptions{Mode: "pixelate", Padding: Float(0.3)}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/blur-faces", "mode=pixelate&padding=0.3", in)

	// No options, and no header: zero faces.
	f = imageFake(t, "image/jpeg", imageHeaders)
	res, err = f.client.BlurFaces(ctx, Bytes("a.jpg", in), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/blur-faces", "", in)
	if res.Faces != 0 {
		t.Errorf("Faces = %d, want 0 when the header is absent", res.Faces)
	}
}

func TestIDPhoto(t *testing.T) {
	in := []byte("portrait")
	ctx := context.Background()

	f := imageFake(t, "image/jpeg", map[string]string{
		"X-Image-Width": "413", "X-Image-Height": "531", "X-Image-Quality": "95",
		"X-Id-Photo-Passed": "false", "X-Id-Photo-Findings": "low_resolution,background_not_plain",
		"X-Id-Photo-Head-Mm": "34.0",
	})
	res, err := f.client.IDPhoto(ctx, Bytes("me.jpg", in), &IDPhotoOptions{
		Document: "us-passport", DPI: 600, Background: "document", Format: FormatJPEG, Quality: 95,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/id-photo", "document=us-passport&dpi=600&background=document&format=jpg&quality=95", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if res.Passed || !reflect.DeepEqual(res.Findings, []string{"low_resolution", "background_not_plain"}) || res.HeadMm != 34 {
		t.Errorf("Passed, Findings, HeadMm = %v, %v, %v", res.Passed, res.Findings, res.HeadMm)
	}
	if res.Width != 413 || res.Height != 531 || res.Quality != 95 || res.ContentType != "image/jpeg" {
		t.Errorf("image = %+v", res.Image)
	}

	// The sizes instead of a document.
	_, err = f.client.IDPhoto(ctx, Bytes("me.jpg", in), &IDPhotoOptions{
		WidthMm: 35, HeightMm: 45, HeadMinMm: 32, HeadMaxMm: 36, EyesMinMm: 28.5, EyesMaxMm: 34.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/id-photo",
		"width_mm=35&height_mm=45&head_min_mm=32&head_max_mm=36&eyes_min_mm=28.5&eyes_max_mm=34.5", in)

	// Nothing found, with an empty findings header: an empty list, not nil.
	f = imageFake(t, "image/jpeg", map[string]string{
		"X-Id-Photo-Passed": "true", "X-Id-Photo-Findings": "", "X-Id-Photo-Head-Mm": "33.5",
	})
	res, err = f.client.IDPhoto(ctx, Bytes("me.jpg", in), &IDPhotoOptions{Document: "uk-passport"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Findings == nil || len(res.Findings) != 0 || res.HeadMm != 33.5 {
		t.Errorf("Passed, Findings, HeadMm = %v, %#v, %v", res.Passed, res.Findings, res.HeadMm)
	}

	// No headers at all: not passed, an empty list, no head size.
	f = imageFake(t, "image/jpeg", imageHeaders)
	res, err = f.client.IDPhoto(ctx, Bytes("me.jpg", in), &IDPhotoOptions{Document: "uk-passport"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Findings == nil || len(res.Findings) != 0 || res.HeadMm != 0 {
		t.Errorf("Passed, Findings, HeadMm = %v, %#v, %v", res.Passed, res.Findings, res.HeadMm)
	}
}

func TestIDPhotos(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `[
		  {"name":"us-passport","title":"United States passport","checked":"2026-11-12",
		   "sources":["https://example.com/us"],"notes":["No glasses."],
		   "width_mm":51,"height_mm":51,"head_min_mm":25,"head_max_mm":35,"eyes_from_bottom_mm":[28.5,35],
		   "background":"#ffffff","alteration":"Submit the original, unedited photo.","later":"x"},
		  {"name":"uk-passport","title":"United Kingdom passport","checked":"2026-11-12",
		   "sources":["https://example.com/uk"],"notes":[],
		   "width_mm":35,"height_mm":45,"head_min_mm":29,"head_max_mm":34,"background":"#d9d9d9"}]`)
	})
	got, err := f.client.IDPhotos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r.Method != "GET" || r.Path != "/api/v1/id-photos" || len(r.Query) != 0 {
		t.Errorf("request = %s %s ?%s", r.Method, r.Path, r.Query.Encode())
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization = %q", got)
	}
	if len(got) != 2 {
		t.Fatalf("documents = %+v", got)
	}
	us, uk := got[0], got[1]
	if us.Name != "us-passport" || us.Title != "United States passport" || us.Checked != "2026-11-12" ||
		us.Sources[0] != "https://example.com/us" || us.Notes[0] != "No glasses." {
		t.Errorf("document = %+v", us)
	}
	if us.WidthMm != 51 || us.HeightMm != 51 || us.HeadMinMm != 25 || us.HeadMaxMm != 35 ||
		!reflect.DeepEqual(us.EyesFromBottomMm, []float64{28.5, 35}) || us.Background != "#ffffff" ||
		us.Alteration != "Submit the original, unedited photo." {
		t.Errorf("document = %+v", us)
	}
	if !strings.Contains(string(us.Raw), `"later":"x"`) {
		t.Errorf("Raw = %s", us.Raw)
	}
	// What a document does not have is left zero.
	if uk.WidthMm != 35 || uk.HeightMm != 45 || uk.EyesFromBottomMm != nil || uk.Alteration != "" || len(uk.Notes) != 0 {
		t.Errorf("document = %+v", uk)
	}
}

func TestDenoiseAndColorize(t *testing.T) {
	f := imageFake(t, "image/webp", imageHeaders)
	ctx := context.Background()
	in := []byte("photo")

	img, err := f.client.Denoise(ctx, Bytes("a.jpg", in), &DenoiseOptions{Strength: 50, Format: FormatWebP, Quality: 80})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/denoise", "strength=50&format=webp&quality=80", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if img.Width != 800 || img.Height != 600 || img.ContentType != "image/webp" {
		t.Errorf("image = %+v", img)
	}
	if _, err := f.client.Denoise(ctx, Bytes("a.jpg", in), nil); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/denoise", "", in)

	img, err = f.client.Colorize(ctx, Bytes("old.jpg", in), &ColorizeOptions{Strength: 72.5, Format: FormatJPEG, Quality: 90})
	if err != nil {
		t.Fatal(err)
	}
	r = wantImageRequest(t, f, "/v1/colorize", "strength=72.5&format=jpg&quality=90", in)
	if len(r.Parts) != 1 || r.Parts[0].FileName != "old.jpg" {
		t.Errorf("parts = %+v", r.Parts)
	}
	if img.Width != 800 || string(img.Bytes) != "encoded image" {
		t.Errorf("image = %+v", img)
	}
	if _, err := f.client.Colorize(ctx, Bytes("old.jpg", in), nil); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/colorize", "", in)
}

func TestSelect(t *testing.T) {
	f := imageFake(t, "image/png", map[string]string{
		"X-Image-Width": "300", "X-Image-Height": "200", "X-Select-Score": "0.972",
	})
	ctx := context.Background()
	in := []byte("photo")

	res, err := f.client.Select(ctx, Bytes("a.jpg", in), &SelectOptions{
		Points:  []Point{{X: 120, Y: 80}, {X: 130.5, Y: 90.25}},
		Exclude: []Point{{X: 10, Y: 10}, {X: 0, Y: 0}},
		Box:     Rect{X: 100, Y: 60, Width: 80, Height: 70},
		Pick:    "largest", Mask: true, Crop: true, Padding: 8, Background: "white",
		Format: FormatPNG, Quality: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/select",
		"point=120,80&point=130.5,90.25&exclude=10,10&exclude=0,0&box=100,60,80,70"+
			"&pick=largest&mask=true&crop=true&padding=8&background=white&format=png&quality=90", in)
	// The points repeat, in order, as Erase's rect does.
	if got, want := r.Query["point"], []string{"120,80", "130.5,90.25"}; !reflect.DeepEqual(got, want) {
		t.Errorf("point = %v, want %v in order", got, want)
	}
	if got, want := r.Query["exclude"], []string{"10,10", "0,0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("exclude = %v, want %v in order", got, want)
	}
	if got, want := r.Query["box"], []string{"100,60,80,70"}; !reflect.DeepEqual(got, want) {
		t.Errorf("box = %v, want %v", got, want)
	}
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if res.Score != 0.972 {
		t.Errorf("Score = %v", res.Score)
	}
	if res.Width != 300 || res.Height != 200 || res.ContentType != "image/png" || string(res.Bytes) != "encoded image" {
		t.Errorf("image = %+v", res.Image)
	}

	// One point, at the photo's corner: no box, no exclude, no zeros.
	if _, err := f.client.Select(ctx, Bytes("a.jpg", in), &SelectOptions{Points: []Point{{}}}); err != nil {
		t.Fatal(err)
	}
	r = wantImageRequest(t, f, "/v1/select", "point=0,0", in)
	if _, ok := r.Query["box"]; ok {
		t.Error("an empty box was sent")
	}

	// A box alone.
	if _, err := f.client.Select(ctx, Bytes("a.jpg", in), &SelectOptions{Box: Rect{Width: 40, Height: 30}}); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/select", "box=0,0,40,30", in)

	// Without the header: zero.
	f = imageFake(t, "image/png", imageHeaders)
	res, err = f.client.Select(ctx, Bytes("a.jpg", in), &SelectOptions{Points: []Point{{X: 5, Y: 6}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != 0 {
		t.Errorf("Score = %v, want 0 when the header is absent", res.Score)
	}
}

func TestRestoreFaces(t *testing.T) {
	f := imageFake(t, "image/jpeg", map[string]string{
		"X-Image-Width": "640", "X-Image-Height": "480", "X-Image-Quality": "90",
		"X-Faces": "2", "X-Faces-Skipped": "1",
	})
	ctx := context.Background()
	in := []byte("old photo")

	res, err := f.client.RestoreFaces(ctx, Bytes("old.jpg", in), &RestoreFacesOptions{
		Strength: 80, MinScore: 0.5, Format: FormatJPEG, Quality: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/restore-faces", "strength=80&min_score=0.5&format=jpg&quality=90", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if res.Faces != 2 || res.FacesSkipped != 1 {
		t.Errorf("Faces, FacesSkipped = %d, %d", res.Faces, res.FacesSkipped)
	}
	if res.Width != 640 || res.Quality != 90 || res.ContentType != "image/jpeg" {
		t.Errorf("image = %+v", res.Image)
	}

	// No options, and no headers: zeros.
	f = imageFake(t, "image/jpeg", imageHeaders)
	res, err = f.client.RestoreFaces(ctx, Bytes("old.jpg", in), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/restore-faces", "", in)
	if res.Faces != 0 || res.FacesSkipped != 0 {
		t.Errorf("Faces, FacesSkipped = %d, %d, want zeros when the headers are absent", res.Faces, res.FacesSkipped)
	}
}

func TestPalette(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"width":320,"height":240,"colors":[
		  {"hex":"#c81018","rgb":[200,16,24],"share":0.62},
		  {"hex":"#ffffff","rgb":[255,255,255],"share":0.38}],"later":1}`)
	})
	ctx := context.Background()
	in := []byte("photo")
	res, err := f.client.Palette(ctx, Bytes("a.png", in), &PaletteOptions{Colors: 2})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/palette", "colors=2", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if res.Width != 320 || res.Height != 240 || len(res.Colors) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if c := res.Colors[0]; c.Hex != "#c81018" || !reflect.DeepEqual(c.RGB, []int{200, 16, 24}) || c.Share != 0.62 {
		t.Errorf("first colour = %+v", c)
	}
	if !strings.Contains(string(res.Raw), `"later":1`) {
		t.Errorf("Raw = %s", res.Raw)
	}

	if _, err := f.client.Palette(ctx, Bytes("a.png", in), nil); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/palette", "", in)
}

func TestFindFaces(t *testing.T) {
	const body = `{"width":512,"height":512,"faces":[
	  {"box":{"x":-4.5,"y":120,"width":150.25,"height":190},
	   "landmarks":{"right_eye":[40.5,180],"left_eye":[110,182.5],"nose":[75,220],"mouth_right":[48,250],"mouth_left":[104,252]},
	   "score":0.93,"roll":-2.5,"later":true}]}`
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) { writeJSON(w, 200, body) })
	ctx := context.Background()
	in := []byte("photo")
	res, err := f.client.FindFaces(ctx, Bytes("a.jpg", in), &FindFacesOptions{MinScore: 0.8})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/faces", "min_score=0.8", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if res.Width != 512 || res.Height != 512 || len(res.Faces) != 1 {
		t.Fatalf("result = %+v", res)
	}
	face := res.Faces[0]
	// A box may start outside the photo.
	if face.Box != (FaceBox{X: -4.5, Y: 120, Width: 150.25, Height: 190}) {
		t.Errorf("box = %+v", face.Box)
	}
	want := FaceLandmarks{
		RightEye: Point{40.5, 180}, LeftEye: Point{110, 182.5}, Nose: Point{75, 220},
		MouthRight: Point{48, 250}, MouthLeft: Point{104, 252},
	}
	if face.Landmarks != want {
		t.Errorf("landmarks = %+v", face.Landmarks)
	}
	if face.Score != 0.93 || face.Roll != -2.5 {
		t.Errorf("score, roll = %v, %v", face.Score, face.Roll)
	}
	if !strings.Contains(string(res.Raw), `"later":true`) {
		t.Errorf("Raw = %s", res.Raw)
	}

	// No faces, no options: an empty list and nothing in the query.
	f = newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"width":64,"height":48,"faces":[]}`)
	})
	res, err = f.client.FindFaces(ctx, Bytes("a.jpg", in), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/faces", "", in)
	if res.Faces == nil || len(res.Faces) != 0 || res.Width != 64 {
		t.Errorf("result = %+v", res)
	}
}

func TestCaption(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *recordedRequest) {
		writeJSON(w, 200, `{"caption":"A woman in a space suit posing for a photo.","detail":"detailed","width":512,"height":384,"later":[1]}`)
	})
	ctx := context.Background()
	in := []byte("photo")
	res, err := f.client.Caption(ctx, Bytes("a.jpg", in), &CaptionOptions{Detail: "detailed"})
	if err != nil {
		t.Fatal(err)
	}
	r := wantImageRequest(t, f, "/v1/caption", "detail=detailed", in)
	if len(r.Parts) != 1 {
		t.Errorf("%d parts, want only the image", len(r.Parts))
	}
	if res.Caption != "A woman in a space suit posing for a photo." || res.Detail != "detailed" || res.Width != 512 || res.Height != 384 {
		t.Errorf("result = %+v", res)
	}
	if !strings.Contains(string(res.Raw), `"later":[1]`) {
		t.Errorf("Raw = %s", res.Raw)
	}

	if _, err := f.client.Caption(ctx, Bytes("a.jpg", in), nil); err != nil {
		t.Fatal(err)
	}
	wantImageRequest(t, f, "/v1/caption", "", in)
}

func TestPointJSON(t *testing.T) {
	data, err := json.Marshal(Point{X: 1.5, Y: 2})
	if err != nil || string(data) != "[1.5,2]" {
		t.Errorf("Marshal = %s, %v", data, err)
	}
	var p Point
	if err := json.Unmarshal([]byte(`[3, 4.25]`), &p); err != nil || p != (Point{3, 4.25}) {
		t.Errorf("Unmarshal = %+v, %v", p, err)
	}
	for _, bad := range []string{`[1]`, `[]`, `{"x":1,"y":2}`, `"1,2"`, `["a","b"]`} {
		if err := json.Unmarshal([]byte(bad), &p); err == nil {
			t.Errorf("Unmarshal(%s): no error", bad)
		}
	}
}
