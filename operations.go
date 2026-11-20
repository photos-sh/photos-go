package photos

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Output formats, for the Format field of the options.
const (
	FormatPNG  = "png"
	FormatJPEG = "jpg"
	FormatWebP = "webp"
	FormatAVIF = "avif"
)

// How Resize fits an image into Width × Height, for ResizeOptions.Fit.
const (
	FitContain = "contain" // keep the proportions, fit inside (the default)
	FitCover   = "cover"   // keep the proportions, fill and crop
	FitFill    = "fill"    // stretch to exactly Width × Height
)

// Which text RemoveText removes, for RemoveTextOptions.Region.
const (
	RegionBackground = "background" // text off the product (the default)
	RegionAll        = "all"        // text on the product too
)

// Background-removal models, for RemoveBackgroundOptions.Model. Each costs
// the same. Model takes any name, so one the API adds later can be sent
// before this library has a constant for it.
const (
	ModelAtlas = "atlas" // everyday product photos (the default)
	ModelLumen = "lumen" // pale packaging on a light backdrop; close-ups
	ModelMuse  = "muse"  // people: portraits, models wearing the product
)

// MaxImagesPerRequest is how many photos ReadLabel and Identify take.
const MaxImagesPerRequest = 8

// In every options struct a field left at its zero value is not sent, and
// the API uses its own default. For most options the zero means nothing
// else: quality, sizes and scale start at 1, a setting of 0 changes nothing,
// and most booleans default to false. The few whose zero is a value of its
// own (a margin of 0, a tolerance of 0, an automatic correction turned off)
// are pointers: nil is not sent, and Int, Float and Bool make one.

// Int returns a pointer to v, for an option that is a *int.
func Int(v int) *int { return &v }

// Float returns a pointer to v, for an option that is a *float64.
func Float(v float64) *float64 { return &v }

// Bool returns a pointer to v, for an option that is a *bool.
func Bool(v bool) *bool { return &v }

// RemoveBackgroundOptions are the options of RemoveBackground. Zero fields
// are not sent.
//
// The fields after Mask stage the cutout in the same call: a canvas with
// the subject at a set size and position, a shadow or a reflection under
// it, and something behind it. Give at most one of Background,
// BackgroundImage and Blur. Mask takes none of them, and Crop goes with
// neither a canvas nor Blur.
type RemoveBackgroundOptions struct {
	// Format is the output format: "png" (the default), "jpg", "webp" or "avif".
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
	// Background composites the cutout onto a colour ("white", "#rrggbb").
	// Empty keeps it transparent (JPEG output defaults to white).
	Background string
	// Crop crops to the subject.
	Crop bool
	// Padding is the margin around the subject when cropping, in pixels.
	Padding int
	// Model is the model that finds the subject. Each costs the same: try
	// another when one drops part of what you sell. Empty leaves the choice
	// to the API, whose default is ModelAtlas.
	//
	//   - ModelAtlas: everyday product photos, a product alone or with its
	//     box, on any backdrop.
	//   - ModelLumen: pale packaging on a light backdrop, such as a cream or
	//     wooden box beside a bottle, and close-ups where the product fills
	//     the photo. On those, atlas can keep the bottle and drop the box.
	//     Lumen may keep a dark backdrop at the photo's edge, and can cut
	//     away part of an open frame or wire stand.
	//   - ModelMuse: people, in portraits and as models wearing the product.
	//
	// Any other name is sent as it is, for a model the API adds later. Model
	// works with Studio.
	Model string
	// Studio is for studio shots: a product on a plain, light backdrop, as
	// in a light tent. It also keeps what clearly stands out from the
	// backdrop and touches the product, such as the gift box, case or lid
	// shown with a bottle, or a pale box the model left half transparent.
	// Shadows and the floor below the product are left out. A photo without
	// a plain light backdrop is cut out as without it.
	Studio bool
	// Mask returns the greyscale alpha mask instead of the cutout.
	Mask bool
	// Width and Height place the subject on a canvas of this size, in
	// pixels. Give both. Without them the photo keeps its size.
	Width  int
	Height int
	// Fill is how much of the canvas the subject may take in its tighter
	// dimension, 0.05 to 1 (default 0.85). It needs Width and Height.
	Fill float64
	// Align is where the subject sits on the canvas: "center" (the
	// default), "top" (hanging from the top margin) or "bottom" (standing on
	// the bottom margin).
	Align string
	// Shadow is "contact" (a soft shadow at the subject's base, as if it
	// stands on a surface), "drop" (the silhouette, offset and blurred) or
	// "none" (the default).
	Shadow string
	// Reflection adds a fading mirror image under the subject. Leave room
	// below it: a canvas with Fill under 1.
	Reflection bool
	// Blur keeps the photo's own background behind the subject, blurred:
	// 1 (slight) to 100.
	Blur float64
	// BackgroundImage is an image to put behind the subject; it is scaled to
	// cover the result. The zero Input means none.
	BackgroundImage Input
}

// ResizeOptions are the options of Resize. Zero fields are not sent.
type ResizeOptions struct {
	// Width and Height are the target size in pixels. Give one to keep the
	// proportions, or both.
	Width  int
	Height int
	// Fit says how to fit when both sides are given: FitContain (default),
	// FitCover or FitFill. Cover and fill need both sides.
	Fit string
	// Enlarge allows scaling up with FitContain.
	Enlarge bool
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100 (WebP 100 is lossless).
	Quality int
	// MaxBytes lowers the quality until the file fits in this many bytes.
	MaxBytes int
	// Background flattens transparency onto this colour.
	Background string
	// Trim cuts away an even border first: whatever matches the top-left
	// pixel, or is transparent like it.
	Trim bool
	// TrimTolerance is how far a pixel may differ from the border's colour
	// and still be border, per channel, 0–255 (default 10). It needs Trim.
	// It is a pointer because 0 (only the exact colour) is a value: use Int.
	TrimTolerance *int
	// DPI is the print resolution to write into a JPEG or PNG, 1–2400. No
	// pixel changes; WebP and AVIF have no such field.
	DPI int
}

// UpscaleOptions are the options of Upscale. Zero fields are not sent.
type UpscaleOptions struct {
	// Scale is the factor, 1 to 4 (default 4).
	Scale float64
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// Rect is an area of an image in pixels, from its top-left corner.
type Rect struct {
	X, Y, Width, Height int
}

// EraseOptions say what Erase removes: the areas in Rects, the white parts
// of Mask, or both. At least one is needed.
type EraseOptions struct {
	// Rects are the areas to remove.
	Rects []Rect
	// Mask is an image the size of the photo, white where to remove. The
	// zero Input means no mask.
	Mask Input
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// RemoveTextOptions are the options of RemoveText. Zero fields are not sent.
type RemoveTextOptions struct {
	// Region is RegionBackground (default: only text off the product) or
	// RegionAll.
	Region string
	// Matching limits removal to lines matching this case-insensitive
	// regular expression, such as `sale|€|www\.`.
	Matching string
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// RunOptions name what Run applies: a built-in preset (see Presets) or a
// recipe.
type RunOptions struct {
	// Preset is the name of a built-in preset, such as "marketplace".
	Preset string
	// Recipe is a recipe as TOML text. Preset gives a preset's, to start
	// from; the format is in the API reference under POST /v1/run.
	Recipe string
}

// CheckOptions are the options of Check. Zero fields are not sent.
type CheckOptions struct {
	// Marketplace checks the photo against that marketplace's rules
	// ("amazon", "ebay", "etsy", …; see Marketplaces).
	Marketplace string
	// Subject finds the product, for framing and background checks.
	Subject bool
	// Text reads the text in the photo.
	Text bool
}

// ReadBarcodesOptions are the options of ReadBarcodes. Zero fields are not
// sent.
type ReadBarcodesOptions struct {
	// AllFormats adds EAN-8, UPC-E, Code 128/39/93, QR, Data Matrix and
	// PDF417 to the retail codes read by default.
	AllFormats bool
	// Engine is "auto" (default), "zbar" or "rxing".
	Engine string
}

// IdentifyOptions are the options of Identify. Zero fields are not sent.
type IdentifyOptions struct {
	// Catalogue names the catalogue to search; it may be empty when your
	// organisation has exactly one.
	Catalogue string
	// Top is how many candidates to return, 1–50 (default 5).
	Top int
}

// EnhanceOptions are the options of Enhance. Without any, Enhance stretches
// the tones to the full range and takes out a colour cast. With any setting,
// only what is given is applied.
type EnhanceOptions struct {
	// Auto is automatic levels and white balance together. It is on when no
	// other setting is given; Bool(true) keeps it beside other settings.
	Auto *bool
	// Levels stretches the tones so the darkest pixels are black and the
	// brightest white. Nil follows Auto.
	Levels *bool
	// WhiteBalance takes out a colour cast. Nil follows Auto.
	WhiteBalance *bool
	// Brightness, Contrast and Saturation go from -100 to 100; zero changes
	// nothing. A Saturation of -100 is black and white.
	Brightness float64
	Contrast   float64
	Saturation float64
	// Temperature goes from -100 (cooler, bluer) to 100 (warmer).
	Temperature float64
	// Sharpen goes from 0 to 100.
	Sharpen float64
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// CropOptions are the options of Crop. Zero fields are not sent.
type CropOptions struct {
	// Aspect is the shape as width:height ("4:5", "16:9", "1:1") or a number
	// ("1.5").
	Aspect string
	// Width and Height scale the crop to exactly this size in pixels; give
	// both. Without Aspect they are the shape as well.
	Width  int
	Height int
	// Zoom crops tightly around the subject: the smallest window of the
	// shape that holds it and its Padding. Without Zoom the crop is the
	// largest window of the shape, moved to hold the subject.
	Zoom bool
	// Padding is the room around the subject with Zoom, as a fraction of
	// its longest side, 0 to 2 (default 0.1). It is a pointer because 0 (no
	// room) is a value: use Float.
	Padding *float64
	// Focus is what the crop is built around: "subject" (the default) or
	// "face" (all the faces in the photo as one area). For a portrait or an
	// avatar use "face" with Zoom and a Padding around 0.6.
	Focus string
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// WatermarkOptions say what Watermark puts on the photo, Text or a
// Watermark image (not both), and where. Zero fields are not sent.
type WatermarkOptions struct {
	// Text is the text to draw, up to 200 characters, in a bold sans-serif
	// with Latin, Greek and Cyrillic letters.
	Text string
	// Watermark is an image to put on the photo, such as a logo; a PNG with
	// transparency works best. The zero Input means none.
	Watermark Input
	// Color is the text's colour: "white" (the default), "black", "#rrggbb".
	Color string
	// Position is "top-left", "top", "top-right", "left", "center", "right",
	// "bottom-left", "bottom" or "bottom-right" (the default).
	Position string
	// Scale is the mark's width as a percentage of the photo's width, 1–100
	// (default 25).
	Scale float64
	// Opacity goes from 1 to 100 (default 50).
	Opacity float64
	// Margin is the distance from the edges as a percentage of the photo's
	// shorter side, 0–50 (default 3). It is a pointer because 0 (against the
	// edge) is a value: use Float.
	Margin *float64
	// Tile repeats the mark across the whole photo. Position is then unused.
	Tile bool
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// ExpandOptions say how Expand grows the canvas: to a shape (Aspect), or by
// pixels on each side, not both. Zero fields are not sent.
type ExpandOptions struct {
	// Aspect is the shape to reach, as width:height ("1:1") or a number. The
	// photo stays in the centre.
	Aspect string
	// Left, Right, Top and Bottom are the pixels to add on each side. A side
	// can grow by at most the photo's own width or height.
	Left   int
	Right  int
	Top    int
	Bottom int
	// Fill is "inpaint" (the default: the new area continues the photo) or a
	// colour ("white", "black", "#rrggbb"), which needs no model.
	Fill string
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// VectorizeOptions are the options of Vectorize. Zero fields are not sent.
type VectorizeOptions struct {
	// Mode is "color" (the default) or "mono", which draws whatever is
	// darker than mid grey as one black shape.
	Mode string
	// Curves is "spline" (smooth curves, the default), "polygon" (straight
	// segments) or "pixel" (every pixel step, for pixel art).
	Curves string
	// Detail is the bits kept per colour channel when grouping pixels, 1–8
	// (default 8). Lower merges similar shades into fewer shapes.
	Detail int
	// Speckle drops areas smaller than this many pixels across, 0–100
	// (default 4). It is a pointer because 0 (keep everything) is a value:
	// use Int.
	Speckle *int
}

// PaletteOptions are the options of Palette. Zero fields are not sent.
type PaletteOptions struct {
	// Colors is how many colours to return at most, 1–16 (default 5).
	Colors int
}

// FindFacesOptions are the options of FindFaces. Zero fields are not sent.
type FindFacesOptions struct {
	// MinScore is how sure the model must be that something is a face,
	// 0.1–1 (default 0.6).
	MinScore float64
}

// BlurFacesOptions are the options of BlurFaces. Zero fields are not sent.
type BlurFacesOptions struct {
	// Mode is "blur" (the default), "pixelate" or "fill". Only "fill" leaves
	// nothing of the face.
	Mode string
	// Strength is how strong the blur or how coarse the mosaic is, 1–100
	// (default 50).
	Strength float64
	// Padding is how far past the face's box the hidden area reaches, as a
	// fraction of the box, 0 to 1 (default 0.15). It is a pointer because 0
	// (the box itself) is a value: use Float.
	Padding *float64
	// Color is the colour for the "fill" mode: "black" (the default),
	// "white", "#rrggbb".
	Color string
	// MinScore is how sure the model must be that something is a face,
	// 0.1–1 (default 0.6). Lower finds more faces and more things that are
	// not.
	MinScore float64
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// IDPhotoOptions say what IDPhoto frames the portrait for: a Document, or
// the photo and head sizes (WidthMm, HeightMm, HeadMinMm and HeadMaxMm), not
// both. Zero fields are not sent.
type IDPhotoOptions struct {
	// Document names a document with built-in rules, such as "us-passport";
	// IDPhotos lists them.
	Document string
	// WidthMm and HeightMm are the photo's size in millimetres, 10–200.
	WidthMm  float64
	HeightMm float64
	// HeadMinMm and HeadMaxMm are the smallest and largest allowed head,
	// chin to crown, in millimetres.
	HeadMinMm float64
	HeadMaxMm float64
	// EyesMinMm and EyesMaxMm are the eyes' allowed distance from the bottom
	// edge, for documents with such a rule. Give both or neither.
	EyesMinMm float64
	EyesMaxMm float64
	// DPI is the pixels per inch of the result, 100–1200 (default 300). It
	// is also written into the file.
	DPI int
	// Background is "keep" (the default: the photo's own background),
	// "document" (the colour the document asks for) or a colour ("white",
	// "#rrggbb"). Anything but "keep" cuts the person out and replaces the
	// background, and many authorities do not accept a photo changed that
	// way.
	Background string
	// Format is the output format; the default is JPEG.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// DenoiseOptions are the options of Denoise. Zero fields are not sent.
type DenoiseOptions struct {
	// Strength is how much of the cleaned result replaces the photo, 1–100:
	// 100 (the default) is all of it, 50 half-way back to the original.
	Strength float64
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// ColorizeOptions are the options of Colorize. Zero fields are not sent.
type ColorizeOptions struct {
	// Strength is how colourful the result is, 1–100: 100 (the default) is
	// the model's colour as it is, lower is paler.
	Strength float64
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// SelectOptions say which object Select cuts out: the one at Points, or in
// Box. At least one point or a box is needed. Coordinates are in the photo's
// pixels, after its EXIF rotation.
type SelectOptions struct {
	// Points are points on the object. More than one adds to the selection.
	Points []Point
	// Exclude are points that must stay outside the selection.
	Exclude []Point
	// Box is a box around the object. The zero Rect means none.
	Box Rect
	// Pick says which of the model's three answers to return: "auto" (the
	// default: the surest one that agrees with every point), "smallest" (the
	// part clicked) or "largest" (the whole thing it belongs to).
	Pick string
	// Mask returns the greyscale mask instead of the cutout. Erase takes it
	// as its Mask, to remove the object.
	Mask bool
	// Crop crops to the object.
	Crop bool
	// Padding is the margin around the object when cropping, in pixels.
	Padding int
	// Background composites the cutout onto a colour ("white", "#rrggbb").
	// Empty keeps it transparent (JPEG output defaults to white).
	Background string
	// Format is the output format: "png" (the default), "jpg", "webp" or "avif".
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// RestoreFacesOptions are the options of RestoreFaces. Zero fields are not
// sent.
type RestoreFacesOptions struct {
	// Strength is how much of the restored face replaces the original,
	// 1–100 (default 100: all of it).
	Strength float64
	// MinScore is how sure the detector must be that something is a face,
	// 0.1–1 (default 0.6).
	MinScore float64
	// Format is the output format; the default is the photo's own.
	Format string
	// Quality is the JPEG/WebP/AVIF quality, 1–100.
	Quality int
}

// CaptionOptions are the options of Caption. Zero fields are not sent.
type CaptionOptions struct {
	// Detail is how much to say: "short" (one sentence, the default),
	// "detailed" (two or three) or "paragraph".
	Detail string
}

// query collects the options that were set.
type query struct{ v url.Values }

func newQuery() *query { return &query{v: url.Values{}} }

func (q *query) str(name, value string) {
	if value != "" {
		q.v.Set(name, value)
	}
}

func (q *query) num(name string, value int) {
	if value != 0 {
		q.v.Set(name, strconv.Itoa(value))
	}
}

func (q *query) float(name string, value float64) {
	if value != 0 {
		q.v.Set(name, formatFloat(value))
	}
}

func (q *query) flag(name string, value bool) {
	if value {
		q.v.Set(name, "true")
	}
}

// The options whose zero is a value: nil is not sent, a zero is.

func (q *query) optNum(name string, value *int) {
	if value != nil {
		q.v.Set(name, strconv.Itoa(*value))
	}
}

func (q *query) optFloat(name string, value *float64) {
	if value != nil {
		q.v.Set(name, formatFloat(*value))
	}
}

func (q *query) optFlag(name string, value *bool) {
	if value != nil {
		q.v.Set(name, strconv.FormatBool(*value))
	}
}

// points adds one name=x,y for each point, in order.
func (q *query) points(name string, points []Point) {
	for _, p := range points {
		q.v.Add(name, formatFloat(p.X)+","+formatFloat(p.Y))
	}
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// post sends images (and any extra parts) as multipart/form-data.
func (c *Client) post(ctx context.Context, path string, q *query, parts []formPart) (*response, error) {
	body, err := multipartBody(parts)
	if err != nil {
		return nil, err
	}
	r := &request{method: http.MethodPost, path: path, body: body, retryConnection: true}
	if q != nil {
		r.query = q.v
	}
	return c.send(ctx, r)
}

func (c *Client) postImage(ctx context.Context, path string, q *query, parts []formPart) (*Image, error) {
	resp, err := c.post(ctx, path, q, parts)
	if err != nil {
		return nil, err
	}
	return newImage(resp), nil
}

func imageParts(images ...Input) []formPart {
	parts := make([]formPart, 0, len(images)+1)
	for _, in := range images {
		parts = append(parts, filePart("image", in))
	}
	return parts
}

func checkImages(images []Input) error {
	if len(images) == 0 {
		return errors.New("photos: no images: give at least one")
	}
	if len(images) > MaxImagesPerRequest {
		return fmt.Errorf("photos: %d images: at most %d photos of one product", len(images), MaxImagesPerRequest)
	}
	return nil
}

// RemoveBackground cuts the subject out of its background, and can stage the
// cutout in the same call: on a canvas, with a shadow or a reflection, in
// front of a colour, an image or the photo's own blurred background. opts
// may be nil.
//
// When the photo has no subject (under 3% of it), the photo comes back as it
// was, with or without a canvas, and its SubjectFound is false.
func (c *Client) RemoveBackground(ctx context.Context, image Input, opts *RemoveBackgroundOptions) (*Image, error) {
	q := newQuery()
	parts := imageParts(image)
	if opts != nil {
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
		q.str("background", opts.Background)
		q.flag("crop", opts.Crop)
		q.num("padding", opts.Padding)
		q.str("model", opts.Model)
		q.flag("studio", opts.Studio)
		q.flag("mask", opts.Mask)
		q.num("width", opts.Width)
		q.num("height", opts.Height)
		q.float("fill", opts.Fill)
		q.str("align", opts.Align)
		q.str("shadow", opts.Shadow)
		q.flag("reflection", opts.Reflection)
		q.float("blur", opts.Blur)
		if !opts.BackgroundImage.IsZero() {
			parts = append(parts, filePart("background", opts.BackgroundImage))
		}
	}
	return c.postImage(ctx, "/v1/remove-background", q, parts)
}

// Resize resizes, converts and compresses an image. It can also trim an
// even border first and write a print resolution. opts may be nil.
func (c *Client) Resize(ctx context.Context, image Input, opts *ResizeOptions) (*Image, error) {
	q := newQuery()
	if opts != nil {
		q.num("width", opts.Width)
		q.num("height", opts.Height)
		q.str("fit", opts.Fit)
		q.flag("enlarge", opts.Enlarge)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
		q.num("max_bytes", opts.MaxBytes)
		q.str("background", opts.Background)
		q.flag("trim", opts.Trim)
		q.optNum("trim_tolerance", opts.TrimTolerance)
		q.num("dpi", opts.DPI)
	}
	return c.postImage(ctx, "/v1/resize", q, imageParts(image))
}

// Upscale enlarges an image with an AI model. opts may be nil.
func (c *Client) Upscale(ctx context.Context, image Input, opts *UpscaleOptions) (*Image, error) {
	q := newQuery()
	if opts != nil {
		q.float("scale", opts.Scale)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	return c.postImage(ctx, "/v1/upscale", q, imageParts(image))
}

// Erase removes objects by painting over the areas in opts.Rects and the
// white parts of opts.Mask.
func (c *Client) Erase(ctx context.Context, image Input, opts *EraseOptions) (*Image, error) {
	q := newQuery()
	parts := imageParts(image)
	if opts != nil {
		for _, r := range opts.Rects {
			q.v.Add("rect", fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.Width, r.Height))
		}
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
		if !opts.Mask.IsZero() {
			parts = append(parts, filePart("mask", opts.Mask))
		}
	}
	return c.postImage(ctx, "/v1/erase", q, parts)
}

// RemoveText paints out text, watermarks and captions: by default only text
// off the product. opts may be nil.
func (c *Client) RemoveText(ctx context.Context, image Input, opts *RemoveTextOptions) (*RemoveTextResult, error) {
	q := newQuery()
	if opts != nil {
		q.str("region", opts.Region)
		q.str("matching", opts.Matching)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	resp, err := c.post(ctx, "/v1/remove-text", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	return newRemoveTextResult(resp), nil
}

// Run applies a preset (opts.Preset) or a recipe (opts.Recipe) to an image:
// several steps on one upload, charged as one request. A recipe's [barcodes]
// and [text] steps read the photo before any step changes it, and what they
// read is in the image's Barcodes and Text.
func (c *Client) Run(ctx context.Context, image Input, opts *RunOptions) (*Image, error) {
	q := newQuery()
	parts := imageParts(image)
	if opts != nil {
		q.str("preset", opts.Preset)
		if opts.Recipe != "" {
			parts = append(parts, recipePart(opts.Recipe))
		}
	}
	return c.postImage(ctx, "/v1/run", q, parts)
}

// Fix makes a photo meet a marketplace's rules ("amazon", "ebay", "etsy",
// "shopify", "google", "allegro", "zalando"; see Marketplaces), and reports
// whether the result passes.
func (c *Client) Fix(ctx context.Context, image Input, marketplace string) (*FixResult, error) {
	if marketplace == "" {
		return nil, errors.New("photos: Fix: no marketplace")
	}
	q := newQuery()
	q.str("marketplace", marketplace)
	resp, err := c.post(ctx, "/v1/fix", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	return newFixResult(resp), nil
}

// Enhance fixes light and colour. With no options it stretches the tones to
// the full range and takes out a colour cast; with any setting, only what is
// given is applied. Transparency is kept. It needs no model, and it does not
// denoise, deblur or recover blown highlights. opts may be nil.
func (c *Client) Enhance(ctx context.Context, image Input, opts *EnhanceOptions) (*Image, error) {
	q := newQuery()
	if opts != nil {
		q.optFlag("auto", opts.Auto)
		q.optFlag("levels", opts.Levels)
		q.optFlag("white_balance", opts.WhiteBalance)
		q.float("brightness", opts.Brightness)
		q.float("contrast", opts.Contrast)
		q.float("saturation", opts.Saturation)
		q.float("temperature", opts.Temperature)
		q.float("sharpen", opts.Sharpen)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	return c.postImage(ctx, "/v1/enhance", q, imageParts(image))
}

// Crop finds the subject (or, with opts.Focus "face", the faces) and crops
// the photo to a shape around it, keeping the background.
//
// The result's SubjectFound is false when nothing was found; the crop is
// then centred.
func (c *Client) Crop(ctx context.Context, image Input, opts *CropOptions) (*Image, error) {
	q := newQuery()
	if opts != nil {
		q.str("aspect", opts.Aspect)
		q.num("width", opts.Width)
		q.num("height", opts.Height)
		q.flag("zoom", opts.Zoom)
		q.optFloat("padding", opts.Padding)
		q.str("focus", opts.Focus)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	return c.postImage(ctx, "/v1/crop", q, imageParts(image))
}

// Watermark puts opts.Text or the image opts.Watermark (a logo) on a photo:
// at one of nine positions, or tiled across it. It needs no model.
func (c *Client) Watermark(ctx context.Context, image Input, opts *WatermarkOptions) (*Image, error) {
	q := newQuery()
	parts := imageParts(image)
	if opts != nil {
		q.str("text", opts.Text)
		q.str("color", opts.Color)
		q.str("position", opts.Position)
		q.float("scale", opts.Scale)
		q.float("opacity", opts.Opacity)
		q.optFloat("margin", opts.Margin)
		q.flag("tile", opts.Tile)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
		if !opts.Watermark.IsZero() {
			parts = append(parts, filePart("watermark", opts.Watermark))
		}
	}
	return c.postImage(ctx, "/v1/watermark", q, parts)
}

// Expand adds room around a photo: to a shape (opts.Aspect), or by pixels on
// each side. The new area continues the photo by inpainting, which suits
// plain and softly textured backgrounds. It does not invent content: a
// subject cut off at the edge stays cut off. Transparency is flattened onto
// white.
func (c *Client) Expand(ctx context.Context, image Input, opts *ExpandOptions) (*Image, error) {
	q := newQuery()
	if opts != nil {
		q.str("aspect", opts.Aspect)
		q.num("left", opts.Left)
		q.num("right", opts.Right)
		q.num("top", opts.Top)
		q.num("bottom", opts.Bottom)
		q.str("fill", opts.Fill)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	return c.postImage(ctx, "/v1/expand", q, imageParts(image))
}

// Vectorize traces an image into SVG: each area of one colour becomes a
// filled path. It is for logos, icons and flat drawings; a photograph traces
// into thousands of paths and a large file. Images over 2000 pixels on a
// side are traced from a smaller copy. It needs no model. opts may be nil.
func (c *Client) Vectorize(ctx context.Context, image Input, opts *VectorizeOptions) (*VectorizeResult, error) {
	q := newQuery()
	if opts != nil {
		q.str("mode", opts.Mode)
		q.str("curves", opts.Curves)
		q.num("detail", opts.Detail)
		q.optNum("speckle", opts.Speckle)
	}
	resp, err := c.post(ctx, "/v1/vectorize", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	return newVectorizeResult(resp), nil
}

// BlurFaces finds every face in a photo and hides it inside an ellipse:
// blurred, pixelated, or covered with a colour. Blur and pixelation make a
// face unrecognisable to a person looking at the photo; only the "fill" mode
// leaves nothing of it. opts may be nil.
//
// A small face, a face seen from behind or one mostly covered may not be
// found: check the result's Faces before relying on it.
func (c *Client) BlurFaces(ctx context.Context, image Input, opts *BlurFacesOptions) (*BlurFacesResult, error) {
	q := newQuery()
	if opts != nil {
		q.str("mode", opts.Mode)
		q.float("strength", opts.Strength)
		q.optFloat("padding", opts.Padding)
		q.str("color", opts.Color)
		q.float("min_score", opts.MinScore)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	resp, err := c.post(ctx, "/v1/blur-faces", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	return newBlurFacesResult(resp), nil
}

// IDPhoto frames a portrait for an identity document: it levels the head,
// scales it to the document's head size and places it on a photo of the
// document's size. Give opts.Document (see IDPhotos) or the sizes.
//
// By default the photo's own background is kept, because passport
// authorities do not accept edited photos: the photo is only turned, scaled
// and cropped. opts.Background replaces it, for uses where that is allowed.
//
// The result's Findings list what the tool could see is wrong. It cannot
// judge expression, glasses, shadows, focus or likeness, so an empty list is
// not an approval. A photo with no face, or more than one, is refused with
// an ErrUnprocessable.
func (c *Client) IDPhoto(ctx context.Context, image Input, opts *IDPhotoOptions) (*IDPhotoResult, error) {
	q := newQuery()
	if opts != nil {
		q.str("document", opts.Document)
		q.float("width_mm", opts.WidthMm)
		q.float("height_mm", opts.HeightMm)
		q.float("head_min_mm", opts.HeadMinMm)
		q.float("head_max_mm", opts.HeadMaxMm)
		q.float("eyes_min_mm", opts.EyesMinMm)
		q.float("eyes_max_mm", opts.EyesMaxMm)
		q.num("dpi", opts.DPI)
		q.str("background", opts.Background)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	resp, err := c.post(ctx, "/v1/id-photo", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	return newIDPhotoResult(resp), nil
}

// Denoise removes sensor noise and the blocks and ringing of heavy JPEG
// compression. It does not sharpen a blurred photo or add detail, and on a
// photo that is already clean it removes a little fine texture: lower
// opts.Strength there, or do not use it. Transparency is kept. opts may be
// nil.
//
// It is slow without a GPU (about half a minute a megapixel on a CPU) and
// takes photos up to 12 megapixels.
func (c *Client) Denoise(ctx context.Context, image Input, opts *DenoiseOptions) (*Image, error) {
	q := newQuery()
	if opts != nil {
		q.float("strength", opts.Strength)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	return c.postImage(ctx, "/v1/denoise", q, imageParts(image))
}

// Colorize adds colour to a black-and-white photo. The colours are guesses
// from what such things usually look like: skin, sky, grass and wood come
// out believable, while the colour of clothes, cars or walls cannot be known
// and is invented. A photo that already has colour is treated as black and
// white: its colours are replaced. Transparency is kept. opts may be nil.
func (c *Client) Colorize(ctx context.Context, image Input, opts *ColorizeOptions) (*Image, error) {
	q := newQuery()
	if opts != nil {
		q.float("strength", opts.Strength)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	return c.postImage(ctx, "/v1/colorize", q, imageParts(image))
}

// Select cuts out the one object at opts.Points or in opts.Box, or returns
// its mask. Where RemoveBackground decides for itself what the subject is,
// here you say.
//
// When nothing is found at the points, the error is an ErrUnprocessable; a
// point outside the photo is an ErrBadRequest.
func (c *Client) Select(ctx context.Context, image Input, opts *SelectOptions) (*SelectResult, error) {
	q := newQuery()
	if opts != nil {
		q.points("point", opts.Points)
		q.points("exclude", opts.Exclude)
		if b := opts.Box; b != (Rect{}) {
			q.v.Set("box", fmt.Sprintf("%d,%d,%d,%d", b.X, b.Y, b.Width, b.Height))
		}
		q.str("pick", opts.Pick)
		q.flag("mask", opts.Mask)
		q.flag("crop", opts.Crop)
		q.num("padding", opts.Padding)
		q.str("background", opts.Background)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	resp, err := c.post(ctx, "/v1/select", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	return newSelectResult(resp), nil
}

// RestoreFaces finds the faces in an old, blurred, small or noisy photo and
// redraws each one sharp; the rest of the photo is not touched. opts may be
// nil.
//
// It invents the detail: the result resembles the person, but the colour of
// the eyes, the teeth and the skin's texture are the model's. Do not use it
// where the face must be exactly the person's own, such as an identity
// document or evidence.
func (c *Client) RestoreFaces(ctx context.Context, image Input, opts *RestoreFacesOptions) (*RestoreFacesResult, error) {
	q := newQuery()
	if opts != nil {
		q.float("strength", opts.Strength)
		q.float("min_score", opts.MinScore)
		q.str("format", opts.Format)
		q.num("quality", opts.Quality)
	}
	resp, err := c.post(ctx, "/v1/restore-faces", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	return newRestoreFacesResult(resp), nil
}

// Check measures a photo's quality and, with opts.Marketplace, checks it
// against that marketplace's rules. opts may be nil.
func (c *Client) Check(ctx context.Context, image Input, opts *CheckOptions) (*CheckResult, error) {
	q := newQuery()
	if opts != nil {
		q.str("marketplace", opts.Marketplace)
		q.flag("subject", opts.Subject)
		q.flag("text", opts.Text)
	}
	resp, err := c.post(ctx, "/v1/check", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	out := &CheckResult{}
	return out, decodeJSON(resp, out)
}

// ReadBarcodes reads the barcodes in a photo. opts may be nil.
func (c *Client) ReadBarcodes(ctx context.Context, image Input, opts *ReadBarcodesOptions) (*BarcodesResult, error) {
	q := newQuery()
	if opts != nil {
		q.flag("all_formats", opts.AllFormats)
		q.str("engine", opts.Engine)
	}
	resp, err := c.post(ctx, "/v1/barcodes", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	out := &BarcodesResult{}
	return out, decodeJSON(resp, out)
}

// ReadText reads the text in a photo (OCR), line by line.
func (c *Client) ReadText(ctx context.Context, image Input) (*TextResult, error) {
	resp, err := c.post(ctx, "/v1/ocr", nil, imageParts(image))
	if err != nil {
		return nil, err
	}
	out := &TextResult{}
	return out, decodeJSON(resp, out)
}

// ReadLabel reads product details from one to eight photos of ONE product's
// label: text and codes from all of them are pooled.
func (c *Client) ReadLabel(ctx context.Context, images []Input) (*LabelResult, error) {
	if err := checkImages(images); err != nil {
		return nil, err
	}
	resp, err := c.post(ctx, "/v1/label", nil, imageParts(images...))
	if err != nil {
		return nil, err
	}
	out := &LabelResult{}
	return out, decodeJSON(resp, out)
}

// Identify matches one to eight photos of ONE product against one of your
// organisation's catalogues (uploaded on the console's Catalogues page).
// Catalogue matching is turned on per organisation: until it is on for
// yours and a catalogue is uploaded, the API answers 404
// (errors.Is(err, ErrNotFound)). opts may be nil.
func (c *Client) Identify(ctx context.Context, images []Input, opts *IdentifyOptions) (*IdentifyResult, error) {
	if err := checkImages(images); err != nil {
		return nil, err
	}
	q := newQuery()
	if opts != nil {
		q.str("catalogue", opts.Catalogue)
		q.num("top", opts.Top)
	}
	resp, err := c.post(ctx, "/v1/identify", q, imageParts(images...))
	if err != nil {
		return nil, err
	}
	out := &IdentifyResult{}
	return out, decodeJSON(resp, out)
}

// Palette lists the colours an image is made of, the most common first.
// Transparent pixels do not count, so a cutout's palette is the subject's.
// It needs no model. opts may be nil.
func (c *Client) Palette(ctx context.Context, image Input, opts *PaletteOptions) (*PaletteResult, error) {
	q := newQuery()
	if opts != nil {
		q.num("colors", opts.Colors)
	}
	resp, err := c.post(ctx, "/v1/palette", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	out := &PaletteResult{}
	return out, decodeJSON(resp, out)
}

// FindFaces says where the faces in a photo are: a box and five landmarks
// each, the surest first. It finds faces; it does not recognise who anyone
// is. opts may be nil.
func (c *Client) FindFaces(ctx context.Context, image Input, opts *FindFacesOptions) (*FacesResult, error) {
	q := newQuery()
	if opts != nil {
		q.float("min_score", opts.MinScore)
	}
	resp, err := c.post(ctx, "/v1/faces", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	out := &FacesResult{}
	return out, decodeJSON(resp, out)
}

// Caption describes a photo in words: one sentence, a few, or a paragraph.
// The same photo always gets the same words. opts may be nil.
//
// The description can be wrong (a colour, a count, what an object is), and
// it is in English only. It does not know names, brands or places unless
// they are written in the photo. Read it before publishing it.
func (c *Client) Caption(ctx context.Context, image Input, opts *CaptionOptions) (*CaptionResult, error) {
	q := newQuery()
	if opts != nil {
		q.str("detail", opts.Detail)
	}
	resp, err := c.post(ctx, "/v1/caption", q, imageParts(image))
	if err != nil {
		return nil, err
	}
	out := &CaptionResult{}
	return out, decodeJSON(resp, out)
}

// Account returns the caller's account: credits left and prices, or
// Unlimited for a server key.
func (c *Client) Account(ctx context.Context) (*Account, error) {
	out := &Account{}
	if err := c.getJSON(ctx, "/v1/account", out); err != nil {
		return nil, err
	}
	return out, nil
}

// Preset gives a built-in preset's recipe as TOML: what RunOptions.Recipe
// takes, so a recipe of your own can start from it.
func (c *Client) Preset(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", errors.New("photos: Preset: no name")
	}
	resp, err := c.send(ctx, &request{
		method:          http.MethodGet,
		path:            "/v1/presets/" + url.PathEscape(name),
		retryConnection: true,
	})
	if err != nil {
		return "", err
	}
	return string(resp.body), nil
}

// Presets lists the built-in presets Run takes.
func (c *Client) Presets(ctx context.Context) ([]Preset, error) {
	var out []Preset
	if err := c.getJSON(ctx, "/v1/presets", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Marketplaces lists the marketplaces Check and Fix know, with their rules.
func (c *Client) Marketplaces(ctx context.Context) ([]Marketplace, error) {
	var out []Marketplace
	if err := c.getJSON(ctx, "/v1/marketplaces", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// IDPhotos lists the documents whose photo rules IDPhoto knows: each one's
// photo size, head size, background and notes, with the official pages the
// rules were read from.
func (c *Client) IDPhotos(ctx context.Context) ([]IDPhotoDocument, error) {
	var out []IDPhotoDocument
	if err := c.getJSON(ctx, "/v1/id-photos", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// WebhookSecret returns the key your job webhooks are signed with; pass its
// Secret to VerifyWebhook.
func (c *Client) WebhookSecret(ctx context.Context) (*WebhookSecret, error) {
	out := &WebhookSecret{}
	if err := c.getJSON(ctx, "/v1/webhook-secret", out); err != nil {
		return nil, err
	}
	return out, nil
}

// Health statuses.
const (
	HealthOK      = "ok"      // ready
	HealthLoading = "loading" // still loading its models
	HealthFailed  = "failed"  // a model could not be loaded
)

// Health reports whether the service is ready: HealthOK, HealthLoading or
// HealthFailed. "loading" and "failed" arrive as a 503, which here is an
// answer, not an error, and is not retried. No API key is sent.
func (c *Client) Health(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, &request{method: http.MethodGet, path: "/health", noAuth: true, noRetry: true})
	if err != nil {
		return "", err
	}
	var out struct {
		Status string `json:"status"`
	}
	ok := resp.status == http.StatusOK || resp.status == http.StatusServiceUnavailable
	if ok && decodeJSON(resp, &out) == nil && out.Status != "" {
		return out.Status, nil
	}
	return "", newAPIError(resp)
}
