package photos_test

// Tests against a real engine. They are skipped unless both
// PHOTOS_TEST_BASE_URL and PHOTOS_TEST_API_KEY are set:
//
//	PHOTOS_TEST_BASE_URL=http://127.0.0.1:8098/api \
//	PHOTOS_TEST_API_KEY=... go test -run Integration -v ./...
//
// The engine must accept job inputs and webhooks on 127.0.0.1 (a development
// engine does; production does not). Images are small and model calls few.

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	photos "github.com/photos-sh/photos-go"
)

func integrationClient(t *testing.T, opts ...photos.Option) *photos.Client {
	t.Helper()
	baseURL, key := os.Getenv("PHOTOS_TEST_BASE_URL"), os.Getenv("PHOTOS_TEST_API_KEY")
	if baseURL == "" || key == "" {
		t.Skip("set PHOTOS_TEST_BASE_URL and PHOTOS_TEST_API_KEY to run the integration tests")
	}
	// The test engine is shared and may be busy: be patient.
	opts = append([]photos.Option{photos.WithBaseURL(baseURL), photos.WithRetries(6)}, opts...)
	c, err := photos.New(key, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func integrationContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// productPNG draws a "product" (a shaded box) on a light backdrop.
func productPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{245, 245, 245, 255}}, image.Point{}, draw.Src)
	for y := h / 4; y < 3*h/4; y++ {
		for x := w / 3; x < 2*w/3; x++ {
			img.Set(x, y, color.RGBA{uint8(150 + (x*60)/w), uint8(40 + (y*50)/h), 30, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodedSize(t *testing.T, data []byte) (int, int, string) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the result is not an image this test can read: %v", err)
	}
	return cfg.Width, cfg.Height, format
}

type onlyReader struct{ r io.Reader }

func (o onlyReader) Read(p []byte) (int, error) { return o.r.Read(p) }

func TestIntegrationService(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)

	status, err := c.Health(ctx)
	if err != nil || status != photos.HealthOK {
		t.Fatalf("Health = %q, %v", status, err)
	}

	acc, err := c.Account(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Account == "" || (!acc.Unlimited && acc.Credits == nil) {
		t.Errorf("account = %+v (%s)", acc, acc.Raw)
	}

	presets, err := c.Presets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range presets {
		if p.Name == "marketplace" && p.Description != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("presets = %+v, want one called marketplace", presets)
	}

	marketplaces, err := c.Marketplaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, m := range marketplaces {
		if m.Name == "amazon" {
			found = true
			if m.Title == "" || m.Checked == "" || len(m.Sources) == 0 || len(m.Rules) == 0 || len(m.Fix) == 0 {
				t.Errorf("amazon = %+v", m)
			}
			if m.Rules[0]["check"] == nil || m.Rules[0]["severity"] == nil {
				t.Errorf("first rule = %v", m.Rules[0])
			}
		}
	}
	if !found {
		t.Error("no amazon marketplace")
	}

	secret, err := c.WebhookSecret(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Secret == "" || secret.Header != photos.SignatureHeader || secret.Scheme == "" {
		t.Errorf("webhook secret = %+v", secret)
	}
}

func TestIntegrationResize(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)
	data := productPNG(t, 320, 240)
	path := filepath.Join(t.TempDir(), "product.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// From a file, with every kind of option.
	img, err := c.Resize(ctx, photos.File(path), &photos.ResizeOptions{
		Width: 100, Height: 100, Fit: photos.FitCover, Format: photos.FormatJPEG, Quality: 80, Background: "white",
	})
	if err != nil {
		t.Fatal(err)
	}
	w, h, format := decodedSize(t, img.Bytes)
	if w != 100 || h != 100 || format != "jpeg" {
		t.Errorf("decoded %dx%d %s", w, h, format)
	}
	if img.Width != 100 || img.Height != 100 || img.Quality != 80 || img.ContentType != "image/jpeg" || img.ServerTiming == "" {
		t.Errorf("image = %dx%d q%d %s timing %q", img.Width, img.Height, img.Quality, img.ContentType, img.ServerTiming)
	}
	out := filepath.Join(t.TempDir(), "out"+img.Extension())
	if err := img.Save(out); err != nil {
		t.Fatal(err)
	}
	if saved, _ := os.ReadFile(out); !bytes.Equal(saved, img.Bytes) || !strings.HasSuffix(out, ".jpg") {
		t.Errorf("Save wrote %d bytes to %s", len(saved), out)
	}

	// From a reader of unknown length (a chunked upload), PNG out: no quality.
	img, err = c.Resize(ctx, photos.Reader("product.png", onlyReader{bytes.NewReader(data)}), &photos.ResizeOptions{Width: 64})
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 64 || img.Height != 48 || img.ContentType != "image/png" || img.Quality != 0 {
		t.Errorf("image = %dx%d q%d %s", img.Width, img.Height, img.Quality, img.ContentType)
	}

	// From bytes, no options at all.
	img, err = c.Resize(ctx, photos.Bytes("", data), nil)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 320 || img.Height != 240 {
		t.Errorf("image = %dx%d", img.Width, img.Height)
	}
}

func TestIntegrationCheckAndBarcodes(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)
	data := productPNG(t, 320, 240)

	// Quality only: no model involved.
	res, err := c.Check(ctx, photos.Bytes("product.png", data), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Metrics.Width != 320 || res.Metrics.Height != 240 || len(res.Metrics.Border.Mean) != 3 {
		t.Errorf("metrics = %+v", res.Metrics)
	}
	if res.Findings == nil || res.Marketplace != nil || len(res.Raw) == 0 {
		t.Errorf("result = %+v", res)
	}
	t.Logf("check: %s", res.Raw)

	codes, err := c.ReadBarcodes(ctx, photos.Bytes("product.png", data), &photos.ReadBarcodesOptions{AllFormats: true, Engine: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if len(codes.Barcodes) != 0 || codes.Variant == "" {
		t.Errorf("barcodes = %+v", codes)
	}
}

func TestIntegrationErrors(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)
	data := productPNG(t, 64, 64)

	var apiErr *photos.Error

	// A parameter the API rejects.
	_, err := c.Resize(ctx, photos.Bytes("p.png", data), &photos.ResizeOptions{Width: 10, Fit: "sideways"})
	if !errors.Is(err, photos.ErrBadRequest) || !errors.As(err, &apiErr) || apiErr.Status != 400 || apiErr.Message == "" {
		t.Errorf("bad fit: err = %v", err)
	}

	// Not an image.
	_, err = c.Resize(ctx, photos.Bytes("p.png", []byte("this is not an image")), nil)
	if !errors.As(err, &apiErr) || (apiErr.Status != 400 && apiErr.Status != 415) {
		t.Errorf("not an image: err = %v", err)
	}

	// A marketplace that does not exist.
	_, err = c.Check(ctx, photos.Bytes("p.png", data), &photos.CheckOptions{Marketplace: "nowhere"})
	if !errors.Is(err, photos.ErrBadRequest) {
		t.Errorf("unknown marketplace: err = %v", err)
	}

	// A job that does not exist.
	_, err = c.Jobs.Get(ctx, "job_00000000000000000000000000000000")
	if !errors.Is(err, photos.ErrNotFound) || !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Errorf("unknown job: err = %v", err)
	}
	if err := c.Jobs.Delete(ctx, "job_00000000000000000000000000000000"); !errors.Is(err, photos.ErrNotFound) {
		t.Errorf("deleting an unknown job: err = %v", err)
	}

	// A wrong key.
	bad, err := photos.New("not-a-key", photos.WithBaseURL(os.Getenv("PHOTOS_TEST_BASE_URL")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = bad.Account(ctx)
	if !errors.Is(err, photos.ErrUnauthorized) || !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Errorf("wrong key: err = %v", err)
	}
	// Health needs no key.
	if status, err := bad.Health(ctx); err != nil || status != photos.HealthOK {
		t.Errorf("Health with a wrong key = %q, %v", status, err)
	}
}

// The model-backed tools, one small call each.
func TestIntegrationModels(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)
	data := productPNG(t, 256, 192)
	in := func() photos.Input { return photos.Bytes("product.png", data) }

	t.Run("RemoveBackground", func(t *testing.T) {
		img, err := c.RemoveBackground(ctx, in(), &photos.RemoveBackgroundOptions{Crop: true, Padding: 4})
		if err != nil {
			t.Fatal(err)
		}
		w, h, format := decodedSize(t, img.Bytes)
		if format != "png" || img.ContentType != "image/png" || img.Width != w || img.Height != h || img.Quality != 0 {
			t.Errorf("image = %dx%d %s, decoded %dx%d %s", img.Width, img.Height, img.ContentType, w, h, format)
		}
		if w > 256 || h > 192 {
			t.Errorf("cropping made it larger: %dx%d", w, h)
		}
	})

	t.Run("Erase", func(t *testing.T) {
		mask := image.NewGray(image.Rect(0, 0, 256, 192))
		draw.Draw(mask, image.Rect(200, 150, 230, 180), image.White, image.Point{}, draw.Src)
		var buf bytes.Buffer
		if err := png.Encode(&buf, mask); err != nil {
			t.Fatal(err)
		}
		img, err := c.Erase(ctx, in(), &photos.EraseOptions{
			Rects:  []photos.Rect{{X: 10, Y: 10, Width: 30, Height: 20}, {X: 60, Y: 10, Width: 20, Height: 20}},
			Mask:   photos.Bytes("mask.png", buf.Bytes()),
			Format: photos.FormatJPEG,
		})
		if err != nil {
			t.Fatal(err)
		}
		if w, h, format := decodedSize(t, img.Bytes); w != 256 || h != 192 || format != "jpeg" || img.Quality == 0 {
			t.Errorf("image = %dx%d %s q%d", w, h, format, img.Quality)
		}
	})

	t.Run("Upscale", func(t *testing.T) {
		img, err := c.Upscale(ctx, photos.Bytes("small.png", productPNG(t, 48, 32)), &photos.UpscaleOptions{Scale: 2})
		if err != nil {
			t.Fatal(err)
		}
		if img.Width != 96 || img.Height != 64 {
			t.Errorf("image = %dx%d, want 96x64", img.Width, img.Height)
		}
	})

	t.Run("Run", func(t *testing.T) {
		recipe := "[output]\nformat = \"webp\"\nquality = 70\nmax_side = 100\n"
		img, err := c.Run(ctx, in(), &photos.RunOptions{Recipe: recipe})
		if err != nil {
			t.Fatal(err)
		}
		if img.ContentType != "image/webp" || img.Width != 100 || img.Height != 75 || img.Quality != 70 || img.Extension() != ".webp" {
			t.Errorf("image = %dx%d %s q%d", img.Width, img.Height, img.ContentType, img.Quality)
		}

		img, err = c.Run(ctx, in(), &photos.RunOptions{Preset: "web"})
		if err != nil {
			t.Fatal(err)
		}
		if img.ContentType != "image/webp" {
			t.Errorf("preset web gave %s", img.ContentType)
		}

		_, err = c.Run(ctx, in(), &photos.RunOptions{Preset: "no-such-preset"})
		var apiErr *photos.Error
		if !errors.As(err, &apiErr) {
			t.Errorf("unknown preset: err = %v", err)
		} else {
			t.Logf("unknown preset: %v", err)
		}
	})

	t.Run("ReadText and RemoveText", func(t *testing.T) {
		text, err := c.ReadText(ctx, in())
		if err != nil {
			t.Fatal(err)
		}
		if text.Lines == nil || len(text.Raw) == 0 {
			t.Errorf("result = %+v (%s)", text, text.Raw)
		}

		res, err := c.RemoveText(ctx, in(), &photos.RemoveTextOptions{Region: photos.RegionAll})
		if err != nil {
			t.Fatal(err)
		}
		if res.Header.Get("X-Text-Removed") == "" || res.Header.Get("X-Text-Kept") == "" {
			t.Errorf("no text headers: %v", res.Header)
		}
		// What it finds in a drawn box is the engine's business.
		if res.Width != 256 || res.Height != 192 || res.Removed < 0 || res.Kept < 0 {
			t.Errorf("result = %dx%d, removed %d, kept %d", res.Width, res.Height, res.Removed, res.Kept)
		}
	})

	t.Run("ReadLabel", func(t *testing.T) {
		res, err := c.ReadLabel(ctx, []photos.Input{in(), photos.Bytes("back.png", productPNG(t, 128, 128))})
		if err != nil {
			t.Fatal(err)
		}
		if res.Barcodes == nil || res.Text == nil || len(res.Raw) == 0 {
			t.Errorf("result = %+v (%s)", res, res.Raw)
		}
		t.Logf("label: %s", res.Raw)
	})

	t.Run("Fix", func(t *testing.T) {
		res, err := c.Fix(ctx, in(), "ebay")
		if err != nil {
			t.Fatal(err)
		}
		if res.Header.Get("X-Marketplace-Passed") == "" {
			t.Errorf("no verdict header: %v", res.Header)
		}
		if res.Findings == nil || res.Width == 0 || res.Height == 0 || len(res.Bytes) == 0 {
			t.Errorf("result = %dx%d passed %v findings %v", res.Width, res.Height, res.Passed, res.Findings)
		}
		if res.Passed != (res.Header.Get("X-Marketplace-Passed") == "true") {
			t.Errorf("Passed = %v, header %q", res.Passed, res.Header.Get("X-Marketplace-Passed"))
		}
		t.Logf("fix: %dx%d %s passed=%v findings=%v", res.Width, res.Height, res.ContentType, res.Passed, res.Findings)

		// With a marketplace, Check gives a verdict.
		check, err := c.Check(ctx, in(), &photos.CheckOptions{Marketplace: "ebay"})
		if err != nil {
			t.Fatal(err)
		}
		if v := check.Marketplace; v == nil || v.Marketplace != "ebay" || v.Findings == nil || v.NotChecked == nil {
			t.Errorf("verdict = %+v (%s)", v, check.Raw)
		}
	})
}

// The editing tools that need no model, one small call each, and the new
// options of Resize.
func TestIntegrationEditing(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)
	data := productPNG(t, 256, 192)
	in := func() photos.Input { return photos.Bytes("product.png", data) }

	t.Run("Resize trim and DPI", func(t *testing.T) {
		// The even backdrop is the border: what is left is the box.
		img, err := c.Resize(ctx, in(), &photos.ResizeOptions{Trim: true, TrimTolerance: photos.Int(0), DPI: 300})
		if err != nil {
			t.Fatal(err)
		}
		w, h, format := decodedSize(t, img.Bytes)
		if format != "png" || w != img.Width || h != img.Height || w == 0 || w >= 256 || h >= 192 {
			t.Errorf("image = %dx%d, decoded %dx%d %s; want the border trimmed", img.Width, img.Height, w, h, format)
		}
	})

	t.Run("Enhance", func(t *testing.T) {
		img, err := c.Enhance(ctx, in(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if img.Width != 256 || img.Height != 192 || img.ContentType != "image/png" {
			t.Errorf("image = %dx%d %s", img.Width, img.Height, img.ContentType)
		}
		// A negative setting, a false and a format.
		img, err = c.Enhance(ctx, in(), &photos.EnhanceOptions{
			Auto: photos.Bool(false), Brightness: -10, Sharpen: 20, Format: photos.FormatJPEG,
		})
		if err != nil {
			t.Fatal(err)
		}
		if w, h, format := decodedSize(t, img.Bytes); w != 256 || h != 192 || format != "jpeg" || img.Quality == 0 {
			t.Errorf("image = %dx%d %s q%d", w, h, format, img.Quality)
		}
	})

	t.Run("Watermark", func(t *testing.T) {
		img, err := c.Watermark(ctx, in(), &photos.WatermarkOptions{
			Text: "Acme", Position: "center", Opacity: 80, Margin: photos.Float(0),
		})
		if err != nil {
			t.Fatal(err)
		}
		if img.Width != 256 || img.Height != 192 {
			t.Errorf("image = %dx%d", img.Width, img.Height)
		}
		// A logo, as a second image.
		img, err = c.Watermark(ctx, in(), &photos.WatermarkOptions{
			Watermark: photos.Bytes("logo.png", productPNG(t, 32, 32)), Scale: 20, Tile: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if img.Width != 256 || img.Height != 192 {
			t.Errorf("image = %dx%d", img.Width, img.Height)
		}
	})

	t.Run("Expand", func(t *testing.T) {
		// A colour fill needs no model.
		img, err := c.Expand(ctx, in(), &photos.ExpandOptions{Left: 16, Right: 16, Fill: "white"})
		if err != nil {
			t.Fatal(err)
		}
		if w, h, _ := decodedSize(t, img.Bytes); w != 288 || h != 192 || img.Width != 288 || img.Height != 192 {
			t.Errorf("image = %dx%d, decoded %dx%d, want 288x192", img.Width, img.Height, w, h)
		}
	})

	t.Run("Vectorize", func(t *testing.T) {
		res, err := c.Vectorize(ctx, in(), &photos.VectorizeOptions{Curves: "polygon", Speckle: photos.Int(0)})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(res.ContentType, "image/svg+xml") || res.Extension() != ".svg" || !bytes.Contains(res.Bytes, []byte("<svg")) {
			t.Errorf("result = %s, %d bytes", res.ContentType, len(res.Bytes))
		}
		if res.Header.Get("X-Paths") == "" || res.Paths == 0 || res.Width != 256 || res.Height != 192 {
			t.Errorf("paths %d (header %q), traced at %dx%d", res.Paths, res.Header.Get("X-Paths"), res.Width, res.Height)
		}
	})

	t.Run("Palette", func(t *testing.T) {
		res, err := c.Palette(ctx, in(), &photos.PaletteOptions{Colors: 3})
		if err != nil {
			t.Fatal(err)
		}
		if res.Width != 256 || res.Height != 192 || len(res.Colors) == 0 || len(res.Colors) > 3 || len(res.Raw) == 0 {
			t.Fatalf("result = %+v (%s)", res, res.Raw)
		}
		first := res.Colors[0]
		if !strings.HasPrefix(first.Hex, "#") || len(first.RGB) != 3 || first.Share <= 0 || first.Share > 1 {
			t.Errorf("first colour = %+v", first)
		}
		t.Logf("palette: %s", res.Raw)
	})
}

// The editing tools that use a model, one small call each. The test photo is
// a drawn box: it has a subject and no face.
func TestIntegrationEditingModels(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)
	data := productPNG(t, 256, 192)
	in := func() photos.Input { return photos.Bytes("product.png", data) }

	t.Run("RemoveBackground staged", func(t *testing.T) {
		img, err := c.RemoveBackground(ctx, in(), &photos.RemoveBackgroundOptions{
			Width: 200, Height: 160, Fill: 0.8, Align: "bottom", Shadow: "contact",
			BackgroundImage: photos.Bytes("backdrop.png", productPNG(t, 64, 64)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if w, h, format := decodedSize(t, img.Bytes); w != 200 || h != 160 || format != "png" || img.Width != 200 || img.Height != 160 {
			t.Errorf("image = %dx%d, decoded %dx%d %s, want the 200x160 canvas", img.Width, img.Height, w, h, format)
		}
		if img.SubjectFound != nil {
			t.Logf("X-Subject-Found: %v", *img.SubjectFound)
		}
	})

	t.Run("Crop", func(t *testing.T) {
		img, err := c.Crop(ctx, in(), &photos.CropOptions{
			Aspect: "1:1", Zoom: true, Padding: photos.Float(0), Width: 120, Height: 120,
		})
		if err != nil {
			t.Fatal(err)
		}
		if w, h, _ := decodedSize(t, img.Bytes); w != 120 || h != 120 || img.Width != 120 || img.Height != 120 {
			t.Errorf("image = %dx%d, decoded %dx%d, want 120x120", img.Width, img.Height, w, h)
		}
		if img.SubjectFound == nil {
			t.Errorf("no X-Subject-Found header: %v", img.Header)
		} else {
			t.Logf("crop: %dx%d, subject found: %v", img.Width, img.Height, *img.SubjectFound)
		}
	})

	t.Run("FindFaces and BlurFaces", func(t *testing.T) {
		faces, err := c.FindFaces(ctx, in(), &photos.FindFacesOptions{MinScore: 0.9})
		if err != nil {
			t.Fatal(err)
		}
		if faces.Width != 256 || faces.Height != 192 || faces.Faces == nil || len(faces.Raw) == 0 {
			t.Errorf("result = %+v (%s)", faces, faces.Raw)
		}

		res, err := c.BlurFaces(ctx, in(), &photos.BlurFacesOptions{Mode: "fill", Padding: photos.Float(0), MinScore: 0.9})
		if err != nil {
			t.Fatal(err)
		}
		if res.Header.Get("X-Faces") == "" {
			t.Errorf("no X-Faces header: %v", res.Header)
		}
		if res.Width != 256 || res.Height != 192 || res.Faces != len(faces.Faces) {
			t.Errorf("result = %dx%d, %d faces hidden, %d found", res.Width, res.Height, res.Faces, len(faces.Faces))
		}
	})

	t.Run("IDPhoto and IDPhotos", func(t *testing.T) {
		documents, err := c.IDPhotos(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(documents) == 0 {
			t.Fatal("no documents")
		}
		for _, d := range documents {
			if d.Name == "" || d.Title == "" || d.Checked == "" || len(d.Sources) == 0 || d.Background == "" ||
				d.WidthMm <= 0 || d.HeightMm <= 0 || d.HeadMinMm <= 0 || d.HeadMaxMm < d.HeadMinMm || len(d.Raw) == 0 {
				t.Errorf("document = %+v", d)
			}
		}

		// A drawn box has no face: the photo is refused.
		res, err := c.IDPhoto(ctx, in(), &photos.IDPhotoOptions{Document: documents[0].Name})
		switch {
		case errors.Is(err, photos.ErrUnprocessable):
			t.Logf("no face: %v", err)
		case err != nil:
			t.Fatal(err)
		default:
			if res.Header.Get("X-Id-Photo-Passed") == "" || res.Findings == nil || res.HeadMm <= 0 || res.Width == 0 {
				t.Errorf("result = %dx%d passed %v findings %v head %v mm", res.Width, res.Height, res.Passed, res.Findings, res.HeadMm)
			}
		}

		// The sizes instead of a document reach the same answer.
		_, err = c.IDPhoto(ctx, in(), &photos.IDPhotoOptions{WidthMm: 35, HeightMm: 45, HeadMinMm: 32, HeadMaxMm: 36, DPI: 300})
		if err != nil && !errors.Is(err, photos.ErrUnprocessable) {
			t.Errorf("custom sizes: err = %v", err)
		}
	})

	t.Run("Denoise", func(t *testing.T) {
		// Small: the model is slow on a CPU.
		img, err := c.Denoise(ctx, photos.Bytes("small.png", productPNG(t, 64, 48)), &photos.DenoiseOptions{Strength: 50})
		if err != nil {
			t.Fatal(err)
		}
		if img.Width != 64 || img.Height != 48 {
			t.Errorf("image = %dx%d, want 64x48", img.Width, img.Height)
		}
	})

	t.Run("Colorize", func(t *testing.T) {
		img, err := c.Colorize(ctx, in(), &photos.ColorizeOptions{Strength: 80, Format: photos.FormatJPEG})
		if err != nil {
			t.Fatal(err)
		}
		if w, h, format := decodedSize(t, img.Bytes); w != 256 || h != 192 || format != "jpeg" {
			t.Errorf("decoded %dx%d %s", w, h, format)
		}
	})

	t.Run("Select", func(t *testing.T) {
		// A point on the box and one on the backdrop.
		res, err := c.Select(ctx, in(), &photos.SelectOptions{
			Points:  []photos.Point{{X: 128, Y: 96}},
			Exclude: []photos.Point{{X: 8, Y: 8}},
			Mask:    true,
		})
		// What the model finds in a drawn box is the engine's business.
		if errors.Is(err, photos.ErrUnprocessable) {
			t.Skipf("nothing found at the point: %v", err)
		}
		if err != nil {
			t.Fatal(err)
		}
		if res.Header.Get("X-Select-Score") == "" || res.Score <= 0 {
			t.Errorf("score = %v (header %q)", res.Score, res.Header.Get("X-Select-Score"))
		}
		if w, h, format := decodedSize(t, res.Bytes); w != 256 || h != 192 || format != "png" {
			t.Errorf("mask = %dx%d %s", w, h, format)
		}

		// A point outside the photo is a bad request.
		_, err = c.Select(ctx, in(), &photos.SelectOptions{Points: []photos.Point{{X: 5000, Y: 5000}}})
		if !errors.Is(err, photos.ErrBadRequest) {
			t.Errorf("a point outside the photo: err = %v", err)
		}
	})

	t.Run("RestoreFaces", func(t *testing.T) {
		res, err := c.RestoreFaces(ctx, in(), &photos.RestoreFacesOptions{Strength: 80})
		if err != nil {
			t.Fatal(err)
		}
		if res.Header.Get("X-Faces") == "" || res.Header.Get("X-Faces-Skipped") == "" {
			t.Errorf("no face headers: %v", res.Header)
		}
		if res.Width != 256 || res.Height != 192 || res.Faces != 0 || res.FacesSkipped != 0 {
			t.Errorf("result = %dx%d, %d restored, %d skipped", res.Width, res.Height, res.Faces, res.FacesSkipped)
		}
	})

	t.Run("Caption", func(t *testing.T) {
		res, err := c.Caption(ctx, in(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Caption == "" || res.Detail != "short" || res.Width != 256 || res.Height != 192 || len(res.Raw) == 0 {
			t.Errorf("result = %+v (%s)", res, res.Raw)
		}
		t.Logf("caption: %s", res.Caption)
	})
}

type delivery struct {
	body   []byte
	header string
}

// A job of uploads, with a webhook, from creation to deletion.
func TestIntegrationJobUploads(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)

	deliveries := make(chan delivery, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		deliveries <- delivery{body, r.Header.Get(photos.SignatureHeader)}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()

	path := filepath.Join(t.TempDir(), "first.png")
	if err := os.WriteFile(path, productPNG(t, 200, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	job, err := c.Jobs.Create(ctx, &photos.JobRequest{
		Operation: photos.OperationResize,
		Params:    map[string]any{"width": 50, "format": "jpg", "quality": 70, "enlarge": false},
		Images: []photos.Input{
			photos.File(path),
			photos.Bytes("second.png", productPNG(t, 100, 100)),
			photos.Bytes("broken.png", []byte("not an image")),
		},
		WebhookURL: hook.URL + "/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Jobs.Delete(context.Background(), job.ID)

	if !strings.HasPrefix(job.ID, "job_") || job.Operation != photos.OperationResize || job.Counts.Total != 3 || len(job.Items) != 3 {
		t.Fatalf("job = %+v (%s)", job, job.Raw)
	}
	if job.Params != "enlarge=false&format=jpg&quality=70&width=50" {
		t.Errorf("params = %q", job.Params)
	}
	if job.Items[0].Input != "first.png" || job.Items[1].Input != "second.png" {
		t.Errorf("items = %+v", job.Items)
	}
	if job.Webhook == nil || job.Webhook.URL != hook.URL+"/hook" {
		t.Errorf("webhook = %+v", job.Webhook)
	}

	list, err := c.Jobs.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, j := range list {
		if j.ID == job.ID {
			listed = j.Operation == photos.OperationResize && j.Counts.Total == 3 && j.CreatedAt > 0
		}
	}
	if !listed {
		t.Errorf("the job is not in the list: %+v", list)
	}

	done, err := c.Jobs.Wait(ctx, job.ID, &photos.WaitOptions{Interval: 300 * time.Millisecond, Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !done.Completed() || done.FinishedAt == 0 || done.Counts != (photos.JobCounts{Total: 3, Done: 2, Failed: 1}) {
		t.Fatalf("job = %+v (%s)", done, done.Raw)
	}
	first := done.Items[0]
	if first.Status != photos.ItemDone || first.ContentType != "image/jpeg" || first.Bytes == 0 ||
		!strings.HasSuffix(first.Result, "/v1/jobs/"+job.ID+"/items/0") || first.Headers["x-image-width"] != "50" {
		t.Errorf("first item = %+v", first)
	}
	broken := done.Items[2]
	if broken.Status != photos.ItemFailed || broken.Error == nil || broken.Error.Code == "" || broken.Error.Message == "" {
		t.Errorf("broken item = %+v", broken)
	} else {
		t.Logf("broken item: %v", broken.Error)
	}

	res, err := c.Jobs.Item(ctx, job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	img := res.Image()
	if !res.IsImage() || img == nil || img.Width != 50 || img.Height != 25 || img.Quality != 70 || img.ContentType != "image/jpeg" {
		t.Fatalf("item result = %s %v, image %+v", res.ContentType, res.Header, img)
	}
	if w, h, format := decodedSize(t, img.Bytes); w != 50 || h != 25 || format != "jpeg" {
		t.Errorf("decoded %dx%d %s", w, h, format)
	}
	// A failed item has no result.
	if _, err := c.Jobs.Item(ctx, job.ID, 2); !errors.Is(err, photos.ErrNotFound) {
		t.Errorf("result of the failed item: err = %v", err)
	}

	// The webhook arrives signed with the account's secret.
	secret, err := c.WebhookSecret(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-deliveries:
		event, err := photos.VerifyWebhook(d.body, d.header, secret.Secret, 0)
		if err != nil {
			t.Fatalf("the delivery does not verify: %v (header %q)", err, d.header)
		}
		if event.Type != photos.EventJobCompleted || event.Job == nil || event.Job.ID != job.ID || event.Job.Counts.Done != 2 {
			t.Errorf("event = %+v", event)
		}
		if _, err := photos.VerifyWebhook(d.body, d.header, secret.Secret+"x", 0); !errors.Is(err, photos.ErrWebhookSignature) {
			t.Errorf("wrong secret: err = %v", err)
		}
		if _, err := photos.VerifyWebhookAt(d.body, d.header, secret.Secret, time.Minute, time.Now().Add(time.Hour)); !errors.Is(err, photos.ErrWebhookTimestamp) {
			t.Errorf("an hour later: err = %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Error("no webhook arrived within a minute")
	}

	if err := c.Jobs.Delete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Jobs.Get(ctx, job.ID); !errors.Is(err, photos.ErrNotFound) {
		t.Errorf("after deletion: err = %v", err)
	}
}

// A job of URLs (sent as JSON, with params as an object), with a JSON result.
func TestIntegrationJobURLs(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)

	data := productPNG(t, 160, 120)
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	}))
	defer images.Close()

	job, err := c.Jobs.Create(ctx, &photos.JobRequest{
		Operation: photos.OperationReadBarcodes,
		Params:    map[string]any{"all_formats": true, "engine": "auto"},
		URLs:      []string{images.URL + "/a.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Jobs.Delete(context.Background(), job.ID)
	if job.Params != "all_formats=true&engine=auto" || job.Webhook != nil {
		t.Errorf("job = %+v", job)
	}
	if job.Items[0].URL != images.URL+"/a.png" {
		t.Errorf("item = %+v (%s)", job.Items[0], job.Raw)
	}

	done, err := c.Jobs.Wait(ctx, job.ID, &photos.WaitOptions{Interval: 300 * time.Millisecond, Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if done.Counts.Done != 1 {
		t.Fatalf("job = %s", done.Raw)
	}
	res, err := c.Jobs.Item(ctx, job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var codes photos.BarcodesResult
	if !res.IsJSON() || res.Image() != nil {
		t.Errorf("result is %s", res.ContentType)
	}
	if err := res.JSON(&codes); err != nil {
		t.Fatal(err)
	}
	if codes.Variant == "" || len(codes.Barcodes) != 0 {
		t.Errorf("barcodes = %+v", codes)
	}

	if err := c.Jobs.Delete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
}

// The items of fix, remove-text and vectorize jobs carry the headers of the
// synchronous endpoints. An engine that does not keep them yet skips the
// test.
func TestIntegrationJobItemHeaders(t *testing.T) {
	c := integrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	data := productPNG(t, 256, 192)
	wait := &photos.WaitOptions{Interval: time.Second, Timeout: 10 * time.Minute}

	run := func(t *testing.T, operation string, params map[string]any, header string) (*photos.Job, *photos.JobItemResult) {
		t.Helper()
		job, err := c.Jobs.Create(ctx, &photos.JobRequest{
			Operation: operation, Params: params, Images: []photos.Input{photos.Bytes("product.png", data)},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Jobs.Delete(context.Background(), job.ID) })
		job, err = c.Jobs.Wait(ctx, job.ID, wait)
		if err != nil {
			t.Fatal(err)
		}
		if job.Counts.Done != 1 {
			t.Fatalf("job = %s", job.Raw)
		}
		res, err := c.Jobs.Item(ctx, job.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if res.Header.Get(header) == "" {
			if _, err := res.Fix(); !errors.Is(err, photos.ErrMissingResultHeaders) {
				t.Errorf("Fix without the headers: err = %v", err)
			}
			if _, err := res.RemoveText(); !errors.Is(err, photos.ErrMissingResultHeaders) {
				t.Errorf("RemoveText without the headers: err = %v", err)
			}
			t.Skipf("this engine does not keep %s on job item results (headers: %v)", header, job.Items[0].Headers)
		}
		return job, res
	}

	t.Run("fix", func(t *testing.T) {
		job, res := run(t, photos.OperationFix, map[string]any{"marketplace": "etsy"}, "X-Marketplace-Passed")
		fix, err := res.Fix()
		if err != nil {
			t.Fatal(err)
		}
		headers := job.Items[0].Headers
		if fix.Passed != (headers["x-marketplace-passed"] == "true") || headers["x-marketplace-passed"] == "" {
			t.Errorf("Passed = %v, job view headers %v", fix.Passed, headers)
		}
		if fix.Findings == nil || strings.Join(fix.Findings, ",") != headers["x-marketplace-findings"] {
			t.Errorf("Findings = %v, job view headers %v", fix.Findings, headers)
		}
		if w, h, _ := decodedSize(t, fix.Bytes); w != fix.Width || h != fix.Height || w == 0 {
			t.Errorf("image = %dx%d, decoded %dx%d", fix.Width, fix.Height, w, h)
		}
		if _, err := res.RemoveText(); !errors.Is(err, photos.ErrMissingResultHeaders) {
			t.Errorf("RemoveText on a fix result: err = %v", err)
		}
		t.Logf("fix: %dx%d %s passed=%v findings=%v; job view headers %v", fix.Width, fix.Height, fix.ContentType, fix.Passed, fix.Findings, headers)
	})

	t.Run("remove-text", func(t *testing.T) {
		job, res := run(t, photos.OperationRemoveText, map[string]any{"region": "all"}, "X-Text-Removed")
		rt, err := res.RemoveText()
		if err != nil {
			t.Fatal(err)
		}
		headers := job.Items[0].Headers
		if headers["x-text-removed"] == "" || headers["x-text-kept"] == "" ||
			headers["x-text-removed"] != res.Header.Get("X-Text-Removed") || headers["x-text-kept"] != res.Header.Get("X-Text-Kept") {
			t.Errorf("job view headers %v, result headers %v", headers, res.Header)
		}
		if rt.Width != 256 || rt.Height != 192 || rt.Removed < 0 || rt.Kept < 0 {
			t.Errorf("result = %dx%d removed %d kept %d", rt.Width, rt.Height, rt.Removed, rt.Kept)
		}
		if _, err := res.Fix(); !errors.Is(err, photos.ErrMissingResultHeaders) {
			t.Errorf("Fix on a remove-text result: err = %v", err)
		}
		t.Logf("remove-text: removed %d, kept %d; job view headers %v", rt.Removed, rt.Kept, headers)
	})

	t.Run("vectorize", func(t *testing.T) {
		// In colour: the test photo has nothing darker than mid grey, so a
		// black-only trace of it has no paths.
		job, res := run(t, photos.OperationVectorize, map[string]any{"mode": "color", "speckle": 0}, "X-Paths")
		v, err := res.Vectorize()
		if err != nil {
			t.Fatal(err)
		}
		headers := job.Items[0].Headers
		if headers["x-paths"] == "" || headers["x-paths"] != res.Header.Get("X-Paths") || v.Paths == 0 {
			t.Errorf("Paths = %d, job view headers %v, result headers %v", v.Paths, headers, res.Header)
		}
		if !strings.HasPrefix(v.ContentType, "image/svg+xml") || !bytes.Contains(v.Bytes, []byte("<svg")) || v.Width != 256 || v.Height != 192 {
			t.Errorf("result = %s, %d bytes, traced at %dx%d", v.ContentType, len(v.Bytes), v.Width, v.Height)
		}
		if img := res.Image(); img == nil || img.Extension() != ".svg" {
			t.Errorf("Image = %+v", img)
		}
		if _, err := res.BlurFaces(); !errors.Is(err, photos.ErrMissingResultHeaders) {
			t.Errorf("BlurFaces on a vectorize result: err = %v", err)
		}
		t.Logf("vectorize: %d paths; job view headers %v", v.Paths, headers)
	})
}

// A job with a parameter that repeats (the points of a select job): the
// parameters travel as a query string, with URL inputs too.
func TestIntegrationJobRepeatedParams(t *testing.T) {
	c := integrationClient(t)
	ctx := integrationContext(t)

	data := productPNG(t, 160, 120)
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	}))
	defer images.Close()

	job, err := c.Jobs.Create(ctx, &photos.JobRequest{
		Operation: photos.OperationSelect,
		Params:    map[string]any{"point": []string{"80,60", "84,64"}, "mask": true},
		URLs:      []string{images.URL + "/a.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Jobs.Delete(context.Background(), job.ID)
	if job.Operation != photos.OperationSelect || job.Params != "mask=true&point=80%2C60&point=84%2C64" {
		t.Errorf("job = %s", job.Raw)
	}

	done, err := c.Jobs.Wait(ctx, job.ID, &photos.WaitOptions{Interval: 300 * time.Millisecond, Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	item := done.Items[0]
	if item.Status != photos.ItemDone {
		// Nothing found in a drawn box is the engine's business; a rejected
		// parameter would be ours.
		if item.Error == nil || item.Error.Code != photos.CodeUnprocessable {
			t.Fatalf("item = %+v (%s)", item, done.Raw)
		}
		t.Skipf("nothing found at the points: %v", item.Error)
	}
	res, err := c.Jobs.Item(ctx, job.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	sel, err := res.Select()
	if errors.Is(err, photos.ErrMissingResultHeaders) {
		t.Skipf("this engine does not keep X-Select-Score on job item results (headers: %v)", item.Headers)
	}
	if err != nil {
		t.Fatal(err)
	}
	if sel.Score <= 0 || sel.Width != 160 || sel.Height != 120 || item.Headers["x-select-score"] != res.Header.Get("X-Select-Score") {
		t.Errorf("result = %dx%d score %v; job view headers %v", sel.Width, sel.Height, sel.Score, item.Headers)
	}
}
