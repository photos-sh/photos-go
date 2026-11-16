# photos-go

The Go client library for the [Photos.sh](https://photos.sh) API: image tools
for product photos. Remove backgrounds, resize and compress, upscale, erase
objects and text, check photos against marketplace rules and fix them, and
read barcodes, text and label details. Also: enhance, crop, watermark, expand,
vectorize, denoise and colorize photos, select an object at a point, find,
hide and restore faces, frame identity photos, list colours and write
captions.

- Standard library only: no dependencies.
- Uploads are streamed, from files, bytes or any `io.Reader`.
- Every call takes a `context.Context`.
- Go 1.23 or later.

## Install

```sh
go get github.com/photos-sh/photos-go
```

```go
import photos "github.com/photos-sh/photos-go"
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	photos "github.com/photos-sh/photos-go"
)

func main() {
	// An empty key means: use the PHOTOS_API_KEY environment variable.
	client, err := photos.New("")
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	img, err := client.RemoveBackground(ctx, photos.File("shoe.jpg"), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(img.ContentType, img.Width, img.Height)
	if err := img.Save("shoe.png"); err != nil {
		log.Fatal(err)
	}
}
```

## Images in, images out

An image argument is a `photos.Input`:

```go
photos.File("shoe.jpg")              // a path; opened when the request is sent
photos.Bytes("shoe.jpg", data)       // bytes in memory; the name may be ""
photos.Reader("shoe.jpg", r)         // any io.Reader; read to its end, not closed
```

Methods that change a photo return a `*photos.Image`:

```go
img.Bytes        // the encoded image
img.ContentType  // "image/png", "image/jpeg", "image/webp", "image/avif"
img.Width        // pixels
img.Height
img.Quality      // 1–100; 0 for formats without one (PNG)
img.ServerTiming // the raw Server-Timing header
img.SubjectFound // *bool, from Crop and RemoveBackground; nil means not known
img.Header       // every response header
img.Save(path)   // write it to a file
img.Extension()  // ".png", ".jpg", ".webp", ".avif", ".svg"
```

Some methods report something beside the image (the faces hidden, a score).
Their result holds an `Image`, so it has all of the above too.

Each method takes a pointer to its own options struct, which may be `nil`. A
field left at its zero value is not sent, and the API uses its default. For
most options the zero means nothing else: quality, sizes and scale start at 1,
a setting of 0 changes nothing, and most booleans default to false.

A few options have a zero that is a value of its own: a watermark margin of 0,
a trim tolerance of 0, an automatic correction turned off. Those fields are
pointers. `nil` is not sent, and `photos.Int`, `photos.Float` and
`photos.Bool` make one:

```go
&photos.WatermarkOptions{Text: "Acme", Margin: photos.Float(0)}
```

## Methods

In the examples, `ctx` is a `context.Context` and `client` a `*photos.Client`.

### Remove the background

```go
img, err := client.RemoveBackground(ctx, photos.File("shoe.jpg"), &photos.RemoveBackgroundOptions{
	Background: "white",          // or "#rrggbb"; empty keeps transparency
	Crop:       true,             // crop to the subject
	Padding:    40,               // margin when cropping, in pixels
	Format:     photos.FormatJPEG, // default PNG
	Quality:    90,
	// Mask: true returns the greyscale alpha mask instead.
})
```

For a studio shot (a product on a plain, light backdrop, as in a light tent),
`Studio: true` also keeps what clearly stands out from the backdrop and
touches the product: the gift box, case or lid shown with a bottle, or a pale
box the model left half transparent. Shadows and the floor below the product
are left out. A photo without a plain light backdrop is cut out as without it.

```go
img, err := client.RemoveBackground(ctx, photos.File("perfume.jpg"), &photos.RemoveBackgroundOptions{
	Studio: true, // keep the gift box beside the bottle
})
```

The cutout can be staged in the same call: on a canvas, with a shadow or a
reflection, in front of a colour, an image or the photo's own blurred
background. Give at most one of `Background`, `BackgroundImage` and `Blur`.

```go
img, err := client.RemoveBackground(ctx, photos.File("shoe.jpg"), &photos.RemoveBackgroundOptions{
	Width:           1600,                      // a canvas; give Height too
	Height:          1600,
	Fill:            0.8,                       // how much of it the subject may take (default 0.85)
	Align:           "bottom",                  // "center" (default), "top", "bottom"
	Shadow:          "contact",                 // or "drop"
	Reflection:      true,                      // needs room below: a Fill under 1
	BackgroundImage: photos.File("studio.jpg"), // or Background: "white", or Blur: 40
})
if img.SubjectFound != nil && !*img.SubjectFound {
	// the photo has no subject: it came back as it was, without the canvas
}
```

### Resize, convert, compress

```go
img, err := client.Resize(ctx, photos.File("photo.jpg"), &photos.ResizeOptions{
	Width:    1600,
	Height:   1600,
	Fit:      photos.FitContain, // FitContain (default), FitCover, FitFill
	Enlarge:  false,             // allow scaling up with contain
	Format:   photos.FormatWebP,
	Quality:  82,
	MaxBytes: 300_000,           // lower the quality until it fits
	// Background: "white" flattens transparency.
})
```

It can also cut away an even border first, and write a print resolution:

```go
img, err := client.Resize(ctx, photos.File("scan.png"), &photos.ResizeOptions{
	Trim:          true,          // whatever matches the top-left pixel
	TrimTolerance: photos.Int(0), // 0–255, default 10; 0 is the exact colour only
	DPI:           300,           // written into a JPEG or PNG; no pixel changes
})
```

### Upscale

```go
img, err := client.Upscale(ctx, photos.File("small.jpg"), &photos.UpscaleOptions{
	Scale: 2, // 1 to 4, default 4
})
```

### Erase objects

Give the areas as rectangles, as a mask (an image the size of the photo, white
where to remove), or both.

```go
img, err := client.Erase(ctx, photos.File("room.jpg"), &photos.EraseOptions{
	Rects: []photos.Rect{{X: 40, Y: 60, Width: 200, Height: 120}},
	Mask:  photos.File("room-mask.png"),
})
```

### Remove text, watermarks and captions

By default only text off the product is removed; labels printed on it stay.

```go
res, err := client.RemoveText(ctx, photos.File("listing.jpg"), &photos.RemoveTextOptions{
	Region:   photos.RegionBackground, // or photos.RegionAll
	Matching: `sale|€|www\.`,          // only lines matching this regular expression
})
fmt.Println(res.Removed, res.Kept) // lines painted out, lines left
err = res.Save("listing-clean.jpg") // res has everything an Image has
```

### Enhance light and colour

Without options it stretches the tones to the full range and takes out a
colour cast. With any setting, only what you give is applied.

```go
img, err := client.Enhance(ctx, photos.File("dull.jpg"), nil)

img, err = client.Enhance(ctx, photos.File("dull.jpg"), &photos.EnhanceOptions{
	Auto:       photos.Bool(true), // keep the automatic part beside your settings
	Brightness: 10,                // -100 to 100; so are Contrast, Saturation, Temperature
	Saturation: -15,
	Sharpen:    30,                // 0 to 100
	// Levels and WhiteBalance turn each automatic correction on or off: photos.Bool(false)
})
```

### Crop to a shape

It finds the subject and crops the photo to a shape around it. The background
stays.

```go
img, err := client.Crop(ctx, photos.File("shoe.jpg"), &photos.CropOptions{
	Aspect: "4:5", // or "16:9", "1:1", "1.5"
	// Width: 800, Height: 1000 scale the crop to exactly that size.
})
if img.SubjectFound != nil && !*img.SubjectFound {
	// no subject was found: the crop is centred
}

// An avatar: tight around the face, with room for hair and chin.
avatar, err := client.Crop(ctx, photos.File("portrait.jpg"), &photos.CropOptions{
	Focus:   "face",            // "subject" (default) or "face"
	Zoom:    true,              // the smallest crop that holds it
	Padding: photos.Float(0.6), // room around it, 0–2 (default 0.1)
	Width:   400,
	Height:  400,
})
```

### Add a watermark

Text or a logo, not both. Text is drawn with Latin, Greek and Cyrillic letters.

```go
img, err := client.Watermark(ctx, photos.File("photo.jpg"), &photos.WatermarkOptions{
	Text:     "© Acme",
	Color:    "white",         // or "black", "#rrggbb"
	Position: "bottom-right",  // the default; "top-left", "top", "center", …
	Scale:    25,              // its width, as a percentage of the photo's
	Opacity:  50,              // 1–100
	Margin:   photos.Float(0), // percent of the shorter side, 0–50 (default 3)
})

img, err = client.Watermark(ctx, photos.File("photo.jpg"), &photos.WatermarkOptions{
	Watermark: photos.File("logo.png"), // a PNG with transparency works best
	Tile:      true,                    // repeat it across the whole photo
})
```

### Expand the canvas

```go
img, err := client.Expand(ctx, photos.File("shoe.jpg"), &photos.ExpandOptions{
	Aspect: "16:9", // or Left, Right, Top, Bottom, in pixels
	// Fill: "white" fills with a colour instead of continuing the photo.
})
```

The new area continues the photo, which suits plain and softly textured
backgrounds. It does not invent content: a subject cut off at the edge stays
cut off.

### Trace into SVG

For logos, icons and flat drawings. A photograph traces into thousands of
paths.

```go
res, err := client.Vectorize(ctx, photos.File("logo.png"), &photos.VectorizeOptions{
	Mode:    "color",       // or "mono": one black shape
	Curves:  "spline",      // or "polygon", "pixel"
	Detail:  8,             // 1–8; lower merges similar shades
	Speckle: photos.Int(4), // drop areas smaller than this; 0 keeps everything
})
fmt.Println(res.Paths)     // how many paths the SVG has
err = res.Save("logo.svg") // res.Bytes is the SVG's text, res.ContentType "image/svg+xml"
```

### Hide faces

```go
res, err := client.BlurFaces(ctx, photos.File("street.jpg"), &photos.BlurFacesOptions{
	Mode: "pixelate", // "blur" (default), "pixelate" or "fill"
	// Strength: 50, Padding: photos.Float(0.15), Color: "black", MinScore: 0.6
})
fmt.Println(res.Faces) // how many faces were hidden
```

Check `res.Faces` before relying on the result: a small, turned or covered
face may not be found, and then it still shows. Only `"fill"` leaves nothing
of a face.

### Frame an identity photo

```go
res, err := client.IDPhoto(ctx, photos.File("portrait.jpg"), &photos.IDPhotoOptions{
	Document: "us-passport", // or WidthMm, HeightMm, HeadMinMm and HeadMaxMm
	// DPI: 300; Background: "white" or "document" replaces the background
})
fmt.Println(res.Passed, res.Findings, res.HeadMm) // false ["low_resolution"] 34

documents, err := client.IDPhotos(ctx) // []photos.IDPhotoDocument: Name, Title, WidthMm, HeightMm, …
```

`IDPhoto` keeps the photo's own background by default, because passport
authorities do not accept edited photos. `Findings` lists what the tool could
see is wrong: `head_tilted`, `not_facing_camera`, `head_cut_off`,
`not_enough_room`, `low_resolution`, `background_not_plain`. It cannot judge
expression, glasses, shadows or likeness, so an empty list is not an approval.
A photo with no face, or more than one, is an `ErrUnprocessable`.

### Remove noise

```go
img, err := client.Denoise(ctx, photos.File("grainy.jpg"), &photos.DenoiseOptions{
	Strength: 80, // 1–100: how much of the cleaned result replaces the photo
})
```

`Denoise` is slow without a GPU and takes photos up to 12 megapixels.

### Colour a black-and-white photo

```go
img, err := client.Colorize(ctx, photos.File("grandparents.jpg"), &photos.ColorizeOptions{
	Strength: 80, // 1–100: lower is paler
})
```

`Colorize` guesses colours: skin, sky and grass come out believable, the
colour of clothes or cars is invented.

### Select an object

Where `RemoveBackground` decides what the subject is, here you say: points on
the object, points to keep out of it, or a box around it.

```go
res, err := client.Select(ctx, photos.File("desk.jpg"), &photos.SelectOptions{
	Points:  []photos.Point{{X: 420, Y: 310}}, // on the object; more points add to it
	Exclude: []photos.Point{{X: 600, Y: 300}}, // must stay outside
	// Box: photos.Rect{X: 380, Y: 250, Width: 120, Height: 140}
	// Pick: "smallest" or "largest"; Crop: true; Padding: 10; Background: "white"
})
fmt.Println(res.Score) // how well the model thinks the mask fits, 0 to 1
```

With `Mask: true` the result is the mask, which `Erase` takes as its `Mask` to
remove the object. Nothing found at the points is an `ErrUnprocessable`.

### Restore faces

```go
res, err := client.RestoreFaces(ctx, photos.File("old-family.jpg"), &photos.RestoreFacesOptions{
	Strength: 80, // 1–100: how much of the restored face replaces the original
})
fmt.Println(res.Faces, res.FacesSkipped) // restored; left alone, as sharp enough
```

`RestoreFaces` invents detail: do not use it where the face has to be exactly
the person's, such as an identity document or evidence.

### Run a preset or a recipe

```go
img, err := client.Run(ctx, photos.File("shoe.jpg"), &photos.RunOptions{Preset: "marketplace"})

// Or a recipe of your own, as TOML:
img, err = client.Run(ctx, photos.File("shoe.jpg"), &photos.RunOptions{
	Recipe: "[output]\nformat = \"webp\"\nmax_side = 1600\n",
})

presets, err := client.Presets(ctx) // []photos.Preset{{Name, Description}}
toml, err := client.Preset(ctx, "studio") // the preset's recipe, to start your own from
```

A recipe is one request, charged as one, however many steps it has. Its
`[barcodes]` and `[text]` steps read the photo before any step changes it,
and what they read comes back with the image:

```go
img, err := client.Run(ctx, photos.File("ham.jpg"), &photos.RunOptions{Recipe: `
[barcodes]
[text]
[remove_background]
[background]
color = "white"
[output]
format = "jpg"
`})
for _, code := range img.Barcodes { // nil without [barcodes], empty if none was found
	fmt.Println(code.Format, code.Value)
}
fmt.Println(img.Text, img.TextTruncated) // the lines read; at most 8 KB of them
```

The steps, their options and the rules are in the API reference under
`POST /v1/run`. Reading needs the engine release that added it (16-11-2026);
an older engine refuses `[barcodes]` and `[text]` with a 422.

### Fix a photo for a marketplace

```go
res, err := client.Fix(ctx, photos.File("shoe.jpg"), "amazon")
fmt.Println(res.Passed, res.Findings) // true, [] or false, ["text_overlay"]
err = res.Save("shoe-amazon.jpg")
```

Marketplaces: `amazon`, `ebay`, `etsy`, `shopify`, `google`, `allegro`,
`zalando`. `client.Marketplaces(ctx)` returns each one's rules.

### Check quality and marketplace rules

```go
res, err := client.Check(ctx, photos.File("shoe.jpg"), &photos.CheckOptions{
	Marketplace: "amazon", // also: Subject: true, Text: true
})
fmt.Println(res.Metrics.Width, res.Metrics.Height, res.Metrics.Sharpness)
for _, f := range res.Findings {
	fmt.Println(f.Severity, f.Code, f.Message)
}
if v := res.Marketplace; v != nil {
	fmt.Println(v.Passed, v.Findings, v.NotChecked)
}
```

### Read barcodes

```go
res, err := client.ReadBarcodes(ctx, photos.File("box.jpg"), &photos.ReadBarcodesOptions{
	AllFormats: true, // adds EAN-8, UPC-E, Code 128/39/93, QR, Data Matrix, PDF417
})
for _, b := range res.Barcodes {
	fmt.Println(b.Format, b.Value) // b.GS1 is a *string: nil unless a valid retail code
}
```

### Read text

```go
res, err := client.ReadText(ctx, photos.File("label.jpg"))
for _, line := range res.Lines {
	fmt.Println(line.Text, line.Score, line.Box)
}
```

### Read a label

One to eight photos of one product; text and codes from all of them are pooled.

```go
res, err := client.ReadLabel(ctx, []photos.Input{
	photos.File("front.jpg"),
	photos.File("back.jpg"),
})
label := res.Label
fmt.Println(label.Headline, label.GTIN, label.Ingredients, label.Allergens)
if q := label.NetQuantity; q != nil {
	fmt.Println(q.Value, q.Unit)
}
```

Fields that were not found are zero: an empty string, a nil slice or a nil
pointer.

### Match a product in your catalogue

One to eight photos of one product, matched against one of your
organisation's catalogues. A catalogue is a spreadsheet of your products
(`product_id`, `title`, and where you have them `brand`, `barcode`, `sku`),
uploaded on the console's Catalogues page. Catalogue matching is turned on
per organisation: until it is on for yours and a catalogue is uploaded, the
API answers `404 not_found`. The catalogue's name may be left out when you
have exactly one.

Each catalogue is one kind of product, chosen when it is uploaded: any
product, food and drink, cosmetics, pharmacy and supplements, household and
pet, electronics, or wine and spirits. Each kind also compares its own values
with the label, so the right size or strength of a product sold in several is
found.

Each candidate's `Evidence` says what agreed beyond the name: `barcode`,
`barcode_text`, `sku`, `brand`, and by the catalogue's kind `quantity`,
`pack`, `spf`, `strength`, `count`, `spec`, `model`, `cask_number`, `age`,
`age_weak`, `vintage`, `abv`, `size`. `Label` holds the values read off the
photos.

```go
res, err := client.Identify(ctx,
	[]photos.Input{photos.File("front.jpg"), photos.File("back.jpg")},
	&photos.IdentifyOptions{Catalogue: "shoes", Top: 3})
for _, c := range res.Candidates {
	fmt.Println(c.ProductID, c.Title, c.Score, c.Evidence)
}
```

### List the main colours

```go
res, err := client.Palette(ctx, photos.File("shoe.png"), &photos.PaletteOptions{Colors: 5}) // 1–16
for _, c := range res.Colors {
	fmt.Println(c.Hex, c.RGB, c.Share) // #c81018 [200 16 24] 0.62
}
```

Transparent pixels do not count, so a cutout's palette is the subject's.

### Find faces

```go
res, err := client.FindFaces(ctx, photos.File("group.jpg"), &photos.FindFacesOptions{
	MinScore: 0.8, // how sure the model must be, 0.1–1 (default 0.6)
})
for _, f := range res.Faces {
	fmt.Println(f.Box.X, f.Box.Y, f.Box.Width, f.Box.Height, f.Score, f.Roll)
	fmt.Println(f.Landmarks.RightEye, f.Landmarks.LeftEye, f.Landmarks.Nose) // photos.Point{X, Y}
}
```

It finds faces; it does not recognise who anyone is. "Right" and "left" are
the person's own, so `RightEye` is the one on the left of the photo.

### Describe a photo

```go
res, err := client.Caption(ctx, photos.File("shoe.jpg"), &photos.CaptionOptions{
	Detail: "short", // one sentence (default); "detailed"; "paragraph"
})
fmt.Println(res.Caption)
```

`Caption` can be wrong and is English only: read it before publishing it.

### Account, marketplaces, health

```go
acc, err := client.Account(ctx)
if acc.Unlimited {
	// a server key: not metered
} else if acc.Credits != nil {
	fmt.Println(*acc.Credits, acc.Prices["remove-background"])
}

marketplaces, err := client.Marketplaces(ctx) // Name, Title, Checked, Sources, Notes, Rules, Fix

status, err := client.Health(ctx) // "ok", "loading" or "failed"; sends no key
```

### JSON results and new fields

JSON results are typed structs. Each also keeps the response as it came in a
`Raw` field (`json.RawMessage`), so a field the API adds later can be read
before this library knows it. Loosely specified objects (`Marketplace.Rules`,
`Marketplace.Fix`, `LabelResult.Barcodes`) are `map[string]any`. A
`photos.Point` is an `[x, y]` pair in JSON.

## Errors

```go
img, err := client.Upscale(ctx, photos.File("small.jpg"), nil)

var apiErr *photos.Error
var connErr *photos.ConnectionError
switch {
case err == nil:
	// use img
case errors.Is(err, photos.ErrPaymentRequired):
	// out of credits
case errors.Is(err, photos.ErrBusy):
	// already retried; apiErr.RetryAfter says when to try again
case errors.As(err, &apiErr):
	fmt.Println(apiErr.Status, apiErr.Code, apiErr.Message)
case errors.As(err, &connErr):
	fmt.Println("could not reach the API; timed out:", connErr.Timeout)
case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
	// your context ended
default:
	// something local, such as a file that could not be read
}
```

An error response from the API is a `*photos.Error` with `Status`, `Code`,
`Message`, `RetryAfter` and `Header`. Each code has a constant and a sentinel
for `errors.Is`:

| Code | Status | Constant | Sentinel |
|---|---|---|---|
| `bad_request` | 400 | `CodeBadRequest` | `ErrBadRequest` |
| `unauthorized` | 401 | `CodeUnauthorized` | `ErrUnauthorized` |
| `payment_required` | 402 | `CodePaymentRequired` | `ErrPaymentRequired` |
| `account_suspended` | 403 | `CodeAccountSuspended` | `ErrAccountSuspended` |
| `not_found` | 404 | `CodeNotFound` | `ErrNotFound` |
| `payload_too_large` | 413 | `CodePayloadTooLarge` | `ErrPayloadTooLarge` |
| `unsupported_format` | 415 | `CodeUnsupportedFormat` | `ErrUnsupportedFormat` |
| `unprocessable` | 422 | `CodeUnprocessable` | `ErrUnprocessable` |
| `internal` | 500 | `CodeInternal` | `ErrInternal` |
| `busy` | 503 | `CodeBusy` | `ErrBusy` |
| `model_unavailable` | 503 | `CodeModelUnavailable` | `ErrModelUnavailable` |

A response that is not the API's JSON (from a proxy, say) is still a
`*photos.Error`, with an empty `Code` and the start of the body as `Message`.

A failure to get a response is a `*photos.ConnectionError`; its `Timeout`
field tells a timeout from a broken connection, and `errors.Unwrap` gives the
cause. If your own context is cancelled or its deadline passes, the call
returns `context.Canceled` or `context.DeadlineExceeded` instead.

## Batch jobs

A job applies one operation to many images in the background.

```go
// Images the API fetches…
job, err := client.Jobs.Create(ctx, &photos.JobRequest{
	Operation:  photos.OperationRemoveBackground,
	Params:     map[string]any{"format": "png", "crop": true},
	URLs:       []string{"https://example.com/photos/1.jpg", "https://example.com/photos/2.jpg"},
	WebhookURL: "https://example.com/hooks/photos", // optional
})

// …or images you upload (streamed, like any other upload).
job, err = client.Jobs.Create(ctx, &photos.JobRequest{
	Operation: photos.OperationRun,
	Params:    map[string]any{"preset": "marketplace"},
	Images:    []photos.Input{photos.File("1.jpg"), photos.File("2.jpg")},
})
```

`Params` are the operation's parameters under their HTTP names. Operations:
`OperationRun`, `OperationRemoveBackground`, `OperationResize`,
`OperationUpscale`, `OperationReadText`, `OperationReadBarcodes`,
`OperationCheck`, `OperationFix`, `OperationRemoveText`, `OperationReadLabel`,
`OperationEnhance`, `OperationCrop`, `OperationPalette`, `OperationWatermark`,
`OperationExpand`, `OperationVectorize`, `OperationFindFaces`,
`OperationBlurFaces`, `OperationIDPhoto`, `OperationDenoise`,
`OperationColorize`, `OperationSelect`, `OperationRestoreFaces`,
`OperationCaption`. For `OperationRun`, `Recipe` takes recipe TOML.

A job item is one image, so a `watermark` job takes `text` only and a
`remove-background` job no background image. A parameter that repeats takes a
`[]string`: `"point": []string{"420,310", "440,300"}` for a `select` job.

Wait for it, then collect the results:

```go
job, err = client.Jobs.Wait(ctx, job.ID, &photos.WaitOptions{
	Interval: 5 * time.Second,  // default 2 s
	Timeout:  30 * time.Minute, // default 10 min
})
if errors.Is(err, photos.ErrWaitTimeout) {
	// job is the job as last seen
} else if err != nil {
	log.Fatal(err)
}

for n, item := range job.Items {
	if item.Status != photos.ItemDone {
		fmt.Println(item.Input, "failed:", item.Error) // item.Error.Code, .Message
		continue
	}
	res, err := client.Jobs.Item(ctx, job.ID, n)
	if err != nil {
		log.Fatal(err)
	}
	if img := res.Image(); img != nil { // nil unless the result is an image
		err = img.Save(fmt.Sprintf("result-%d%s", n, img.Extension()))
	} else {
		var text photos.TextResult // an "ocr" job; use the type of the operation
		err = res.JSON(&text)
	}
}
```

`completed` means every item was attempted: `job.Counts` (`Total`, `Done`,
`Failed`, `Pending`) says how they went. An item result also has the raw
`Body`, `ContentType` and `Header`.

The items of a `fix` or `remove-text` job carry what the synchronous methods
report beside the image:

```go
res, err := client.Jobs.Item(ctx, job.ID, 0)

fix, err := res.Fix()       // *photos.FixResult: Passed, Findings and the image
rt, err := res.RemoveText() // *photos.RemoveTextResult: Removed, Kept and the image
if errors.Is(err, photos.ErrMissingResultHeaders) {
	// not a result of that operation, or an engine that does not keep
	// those headers on job items; res.Image() still gives the image
}
```

So do the items of the newer operations. Each of these returns what the
synchronous method does:

```go
img, err := res.RemoveBackground() // *photos.Image; SubjectFound is nil unless the photo had no subject
img, err = res.Crop()              // *photos.Image with its SubjectFound
svg, err := res.Vectorize()        // *photos.VectorizeResult: Paths
blur, err := res.BlurFaces()       // *photos.BlurFacesResult: Faces
id, err := res.IDPhoto()           // *photos.IDPhotoResult: Passed, Findings, HeadMm
sel, err := res.Select()           // *photos.SelectResult: Score
faces, err := res.RestoreFaces()   // *photos.RestoreFacesResult: Faces, FacesSkipped
```

They return that error, never zero values, when the result lacks the headers
they read (`X-Marketplace-Passed`, `X-Text-Removed`, `X-Subject-Found` for a
crop, `X-Paths`, `X-Faces`, `X-Id-Photo-Passed`, `X-Select-Score`,
`X-Faces-Skipped`). `RemoveBackground` is the exception: the API sends
`X-Subject-Found` only when the photo has no subject, so without it the image
comes back with a nil `SubjectFound`. The same headers
are in the job itself, lower-case, as `job.Items[n].Headers`
(`"x-marketplace-passed"`, `"x-marketplace-findings"`, `"x-text-removed"`,
`"x-text-kept"`, `"x-faces"` and so on, beside `"x-image-*"` and
`"server-timing"`).

```go
job, err := client.Jobs.Get(ctx, "job_5f0c…")
jobs, err := client.Jobs.List(ctx)        // newest first, up to 100, without items
err = client.Jobs.Delete(ctx, "job_5f0c…") // cancels it if unfinished
```

## Webhooks

A job with a `WebhookURL` sends a `job.completed` event there when it is done,
signed in the `Photos-Signature` header. Verify each delivery against the raw
body before trusting it; this needs no client.

```go
secret := os.Getenv("PHOTOS_WEBHOOK_SECRET") // once, from client.WebhookSecret(ctx)

http.HandleFunc("/hooks/photos", func(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "cannot read the body", http.StatusBadRequest)
		return
	}
	event, err := photos.VerifyWebhook(body, r.Header.Get(photos.SignatureHeader), secret, 0)
	if err != nil {
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}
	if event.Type == photos.EventJobCompleted {
		log.Printf("job %s: %d done, %d failed", event.Job.ID, event.Job.Counts.Done, event.Job.Counts.Failed)
	}
	w.WriteHeader(http.StatusNoContent)
})
```

The last argument is the tolerance for the delivery's timestamp; zero means
the default, five minutes. `photos.VerifyWebhookAt` takes the current time as
well, for tests. A rejected delivery is a `*photos.WebhookError`: one of
`ErrWebhookHeader` (missing or malformed header), `ErrWebhookSignature`,
`ErrWebhookTimestamp` or `ErrWebhookPayload`.

```go
s, err := client.WebhookSecret(ctx) // s.Secret, s.Header, s.Scheme
```

## Configuration

```go
client, err := photos.New("your-api-key",
	photos.WithBaseURL("http://localhost:8080"), // default https://api.photos.sh
	photos.WithTimeout(60*time.Second),          // per attempt; default 120 s; 0 = none
	photos.WithRetries(4),                       // default 2; 0 = never retry
	photos.WithUserAgent("my-shop/1.2"),         // sent in front of photos-sh-go/<version>
	photos.WithHTTPClient(&http.Client{}),       // your transport, proxy or tracing
)
```

| Setting | Option | Environment | Default |
|---|---|---|---|
| API key | first argument of `New` | `PHOTOS_API_KEY` | none: `New` returns `ErrMissingAPIKey` |
| Base URL | `WithBaseURL` | `PHOTOS_BASE_URL` | `https://api.photos.sh` |
| Timeout | `WithTimeout` | | 120 s per attempt |
| Retries | `WithRetries` | | 2 |

An argument or option wins over the environment. A `*photos.Client` is safe
for use by several goroutines.

### Retries

A request is sent again, up to the number of retries, when

- the API answers `503` with the code `busy`, or with a body that is not the
  API's own error (a proxy in front of it). The wait is the `Retry-After` the
  response gives, at most 30 s; without one, 1 s, then 2 s, doubling.
- no connection could be made or it broke before a response. `Jobs.Create` is
  the exception: the job may have been made, so it is retried only after a 503.

Nothing else is retried: no other status, not a `503 model_unavailable`
(the tool is not on that server), and not a timeout.

Retrying needs the body a second time. `File` and `Bytes` inputs can always be
sent again, and so can a `Reader` that is also an `io.Seeker` (an `*os.File`,
a `*bytes.Reader`): it is rewound to where it was. A request with any other
reader is sent once and never retried, and the first answer (a `busy` error,
for one) is returned as it is. To have such a stream retried, read it into
memory and pass `photos.Bytes`.

### Timeouts

`WithTimeout` bounds one attempt, from sending the request to reading the last
byte of the response. To bound a whole call, retries and waits included, give
it a context with a deadline:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()
img, err := client.Upscale(ctx, photos.File("small.jpg"), nil)
```

## Development

```sh
go build ./...
go vet ./...
gofmt -l .
go test -race ./...
```

The unit tests need no network. The integration tests run against a real
engine and are skipped unless both variables are set:

```sh
PHOTOS_TEST_BASE_URL=http://127.0.0.1:8098/api \
PHOTOS_TEST_API_KEY=your-test-key \
go test -run Integration -v ./...
```

They create jobs whose inputs and webhooks are on `127.0.0.1`, which a
development engine accepts and production does not.

## Licence

MIT. See [LICENSE](LICENSE).
