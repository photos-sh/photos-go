package photos

import (
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Image is an image the API returned.
type Image struct {
	// Bytes is the encoded image.
	Bytes []byte
	// ContentType is its media type, such as "image/png".
	ContentType string
	// Width and Height are its size in pixels (the X-Image-Width and
	// X-Image-Height headers), zero if the response did not say.
	Width  int
	Height int
	// Quality is the quality it was encoded at, 1–100 (X-Image-Quality).
	// It is zero for formats without one, such as PNG.
	Quality int
	// ServerTiming is the raw Server-Timing header: how long each step took.
	ServerTiming string
	// SubjectFound says whether a subject was found in the photo (the
	// X-Subject-Found header). It is nil when the response did not say: nil
	// means "unknown", not "no". Crop always says. RemoveBackground says
	// false when a canvas was asked for and the photo has no subject; the
	// canvas is then empty.
	SubjectFound *bool
	// Header is every header of the response.
	Header http.Header
}

// Save writes the image to the file at path, replacing it if it exists.
func (img *Image) Save(path string) error {
	return os.WriteFile(path, img.Bytes, 0o644)
}

// Extension is a file extension for the image's content type, with the dot
// (".png", ".jpg", ".webp", ".avif", ".svg"), or "" for a type it does not
// know.
func (img *Image) Extension() string {
	switch mediaType(img.ContentType) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	case "image/tiff":
		return ".tiff"
	case "image/svg+xml":
		return ".svg"
	}
	return ""
}

// RemoveTextResult is the image RemoveText returns, and what it did.
type RemoveTextResult struct {
	Image
	// Removed is how many lines of text were painted out (X-Text-Removed).
	Removed int
	// Kept is how many were left in place (X-Text-Kept).
	Kept int
}

// FixResult is the image Fix returns, and the marketplace's verdict on it.
type FixResult struct {
	Image
	// Passed reports whether the fixed image passes the marketplace's check
	// (X-Marketplace-Passed).
	Passed bool
	// Findings are the codes of the findings still present
	// (X-Marketplace-Findings); empty, never nil, when there are none.
	Findings []string
}

// VectorizeResult is the SVG Vectorize returns. Bytes is the SVG's text and
// ContentType "image/svg+xml"; Width and Height are the size the image was
// traced at.
type VectorizeResult struct {
	Image
	// Paths is how many paths the SVG has (X-Paths).
	Paths int
}

// BlurFacesResult is the image BlurFaces returns, and how many faces it hid.
type BlurFacesResult struct {
	Image
	// Faces is how many faces were found and hidden (X-Faces). Check it
	// before relying on the result: a face that was not found still shows.
	Faces int
}

// IDPhotoResult is the photo IDPhoto returns, and what the tool could see is
// wrong with it.
type IDPhotoResult struct {
	Image
	// Passed reports whether Findings is empty (X-Id-Photo-Passed). It is
	// not an approval of the photo: expression, glasses, shadows, focus and
	// likeness are not judged.
	Passed bool
	// Findings are the codes of what was found (X-Id-Photo-Findings):
	// "head_tilted", "not_facing_camera", "head_cut_off", "not_enough_room",
	// "low_resolution", "background_not_plain". Empty, never nil, when
	// there are none.
	Findings []string
	// HeadMm is the head's height in the result, chin to crown, in
	// millimetres (X-Id-Photo-Head-Mm).
	HeadMm float64
}

// SelectResult is the cutout (or the mask) Select returns, and how sure the
// model is of it.
type SelectResult struct {
	Image
	// Score is how well the model thinks the mask fits the object, 0 to 1
	// (X-Select-Score).
	Score float64
}

// RestoreFacesResult is the image RestoreFaces returns, and what it did.
type RestoreFacesResult struct {
	Image
	// Faces is how many faces were restored (X-Faces).
	Faces int
	// FacesSkipped is how many were left alone because they are large and
	// detailed enough already (X-Faces-Skipped).
	FacesSkipped int
}

// The headers that carry what a method reports beside the image.
const (
	headerTextRemoved         = "X-Text-Removed"
	headerTextKept            = "X-Text-Kept"
	headerMarketplacePassed   = "X-Marketplace-Passed"
	headerMarketplaceFindings = "X-Marketplace-Findings"
	headerSubjectFound        = "X-Subject-Found"
	headerPaths               = "X-Paths"
	headerFaces               = "X-Faces"
	headerFacesSkipped        = "X-Faces-Skipped"
	headerSelectScore         = "X-Select-Score"
	headerIDPhotoPassed       = "X-Id-Photo-Passed"
	headerIDPhotoFindings     = "X-Id-Photo-Findings"
	headerIDPhotoHeadMm       = "X-Id-Photo-Head-Mm"
)

func mediaType(contentType string) string {
	t, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(t))
}

func headerInt(h http.Header, name string) int {
	n, err := strconv.Atoi(strings.TrimSpace(h.Get(name)))
	if err != nil {
		return 0
	}
	return n
}

func headerFloat(h http.Header, name string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(h.Get(name)), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

// headerBool is true for a header that says "true", false for anything else.
func headerBool(h http.Header, name string) bool {
	return strings.EqualFold(strings.TrimSpace(h.Get(name)), "true")
}

// headerOptBool is nil for a header that is absent or says neither "true"
// nor "false".
func headerOptBool(h http.Header, name string) *bool {
	var b bool
	switch strings.ToLower(strings.TrimSpace(h.Get(name))) {
	case "true":
		b = true
	case "false":
	default:
		return nil
	}
	return &b
}

// headerList reads a comma-separated header; an absent or empty one gives
// an empty list, not nil.
func headerList(h http.Header, name string) []string {
	list := []string{}
	for _, item := range strings.Split(h.Get(name), ",") {
		if item = strings.TrimSpace(item); item != "" {
			list = append(list, item)
		}
	}
	return list
}

func newImage(resp *response) *Image {
	return &Image{
		Bytes:        resp.body,
		ContentType:  resp.header.Get("Content-Type"),
		Width:        headerInt(resp.header, "X-Image-Width"),
		Height:       headerInt(resp.header, "X-Image-Height"),
		Quality:      headerInt(resp.header, "X-Image-Quality"),
		ServerTiming: strings.Join(resp.header.Values("Server-Timing"), ", "),
		SubjectFound: headerOptBool(resp.header, headerSubjectFound),
		Header:       resp.header,
	}
}

func newRemoveTextResult(resp *response) *RemoveTextResult {
	return &RemoveTextResult{
		Image:   *newImage(resp),
		Removed: headerInt(resp.header, headerTextRemoved),
		Kept:    headerInt(resp.header, headerTextKept),
	}
}

func newFixResult(resp *response) *FixResult {
	return &FixResult{
		Image:    *newImage(resp),
		Passed:   headerBool(resp.header, headerMarketplacePassed),
		Findings: headerList(resp.header, headerMarketplaceFindings),
	}
}

func newVectorizeResult(resp *response) *VectorizeResult {
	return &VectorizeResult{
		Image: *newImage(resp),
		Paths: headerInt(resp.header, headerPaths),
	}
}

func newBlurFacesResult(resp *response) *BlurFacesResult {
	return &BlurFacesResult{
		Image: *newImage(resp),
		Faces: headerInt(resp.header, headerFaces),
	}
}

func newIDPhotoResult(resp *response) *IDPhotoResult {
	return &IDPhotoResult{
		Image:    *newImage(resp),
		Passed:   headerBool(resp.header, headerIDPhotoPassed),
		Findings: headerList(resp.header, headerIDPhotoFindings),
		HeadMm:   headerFloat(resp.header, headerIDPhotoHeadMm),
	}
}

func newSelectResult(resp *response) *SelectResult {
	return &SelectResult{
		Image: *newImage(resp),
		Score: headerFloat(resp.header, headerSelectScore),
	}
}

func newRestoreFacesResult(resp *response) *RestoreFacesResult {
	return &RestoreFacesResult{
		Image:        *newImage(resp),
		Faces:        headerInt(resp.header, headerFaces),
		FacesSkipped: headerInt(resp.header, headerFacesSkipped),
	}
}
