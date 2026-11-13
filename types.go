package photos

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The JSON results. Each top-level result keeps the response as it came in
// its Raw field, so a field the API adds later can be read before this
// library knows it:
//
//	var extra struct{ New string `json:"new"` }
//	json.Unmarshal(result.Raw, &extra)

// keepRaw decodes data into v (a pointer to a type without UnmarshalJSON)
// and returns a copy of data to keep.
func keepRaw(data []byte, v any) (json.RawMessage, error) {
	if err := json.Unmarshal(data, v); err != nil {
		return nil, err
	}
	return bytes.Clone(data), nil
}

// Finding is one thing a check found.
type Finding struct {
	// Code is a stable identifier: "blurry", "too_small",
	// "background_not_white", "text_overlay"…
	Code string `json:"code"`
	// Severity is "info", "warning" or "error"; an error breaks a
	// marketplace rule.
	Severity string `json:"severity"`
	Message  string `json:"message"`
	// Detail is what was measured or found, when there is something to say.
	Detail string `json:"detail,omitempty"`
}

// CheckResult is what Check returns.
type CheckResult struct {
	Metrics  Metrics   `json:"metrics"`
	Findings []Finding `json:"findings"`
	// Text is the text read in the photo, when it was read.
	Text []FoundText `json:"text,omitempty"`
	// Marketplace is the marketplace's verdict, when one was asked for.
	Marketplace *Verdict `json:"marketplace,omitempty"`
	// Ms is how long each step took, in milliseconds.
	Ms map[string]int64 `json:"ms,omitempty"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the result and keeps the JSON in Raw.
func (r *CheckResult) UnmarshalJSON(data []byte) (err error) {
	type plain CheckResult
	r.Raw, err = keepRaw(data, (*plain)(r))
	return err
}

// Metrics are the measurements of a photo.
type Metrics struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// Sharpness is edge crispness; under 0.3 is blurry, under 0.5 soft.
	Sharpness float64 `json:"sharpness"`
	// Noise is in grey levels (above 4 is visible grain); nil when it could
	// not be measured.
	Noise             *float64 `json:"noise"`
	Brightness        float64  `json:"brightness"`
	HighlightsClipped float64  `json:"highlights_clipped"`
	ShadowsClipped    float64  `json:"shadows_clipped"`
	Brightest         float64  `json:"brightest"`
	Border            Border   `json:"border"`
	// Subject is the product, when it was looked for and found.
	Subject *Subject `json:"subject"`
}

// Border describes the edge of a photo: its backdrop.
type Border struct {
	// Mean is the mean colour (red, green, blue).
	Mean []float64 `json:"mean"`
	// White is the share of the border that is pure white.
	White float64 `json:"white"`
	// Cast is (red−green, blue−green) of a light backdrop, or nil.
	Cast []float64 `json:"cast"`
}

// Subject is where the product is in a photo.
type Subject struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
	// Fill is the share of the frame the product spans.
	Fill float64 `json:"fill"`
	Area float64 `json:"area"`
	// Touches lists the edges the product touches: "top", "bottom", "left",
	// "right".
	Touches         []string `json:"touches"`
	BackgroundWhite float64  `json:"background_white"`
}

// FoundText is a piece of text Check read in a photo.
type FoundText struct {
	Text string `json:"text"`
	// OnProduct says whether it is printed on the product; nil when the
	// product was not looked for.
	OnProduct *bool `json:"on_product"`
}

// Verdict is a marketplace's judgement of a photo.
type Verdict struct {
	Marketplace string `json:"marketplace"`
	Title       string `json:"title,omitempty"`
	// Passed is true when no "error" rule is broken and every one was
	// checked.
	Passed   bool      `json:"passed"`
	Findings []Finding `json:"findings"`
	// NotChecked are the rules that could not be checked.
	NotChecked []string `json:"not_checked"`
	// Notes are rules for the seller to check by eye.
	Notes   []string `json:"notes,omitempty"`
	Checked string   `json:"checked,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

// BarcodesResult is what ReadBarcodes returns.
type BarcodesResult struct {
	Barcodes []Barcode `json:"barcodes"`
	// Variant is the preprocessing that found the codes, or "none".
	Variant string `json:"variant"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the result and keeps the JSON in Raw.
func (r *BarcodesResult) UnmarshalJSON(data []byte) (err error) {
	type plain BarcodesResult
	r.Raw, err = keepRaw(data, (*plain)(r))
	return err
}

// Barcode is one code read from a photo.
type Barcode struct {
	// Format is the symbology: "EAN-13", "UPC-A", "ITF", "QR"…
	Format string `json:"format"`
	// Value is the code as decoded.
	Value string `json:"value"`
	// GS1 is the GS1-valid normalised digits of a retail code, else nil.
	GS1 *string `json:"gs1"`
}

// TextResult is what ReadText returns.
type TextResult struct {
	// Lines are the lines of text, top to bottom.
	Lines []TextLine `json:"lines"`
	// Ms is how long each step took, in milliseconds.
	Ms map[string]int64 `json:"ms,omitempty"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the result and keeps the JSON in Raw.
func (r *TextResult) UnmarshalJSON(data []byte) (err error) {
	type plain TextResult
	r.Raw, err = keepRaw(data, (*plain)(r))
	return err
}

// TextLine is one line of text in a photo.
type TextLine struct {
	Text string `json:"text"`
	// Score is the mean character confidence, 0–1.
	Score float64 `json:"score"`
	// Box is the line's corners in pixels (x, y), clockwise from the
	// top-left.
	Box [][]float64 `json:"box"`
}

// LabelResult is what ReadLabel returns.
type LabelResult struct {
	// Label is what was understood; fields not found are left zero.
	Label LabelDetails `json:"label"`
	// Barcodes are the codes found, as the API gives them.
	Barcodes []map[string]any `json:"barcodes"`
	// Text is every line read.
	Text []string `json:"text"`
	// Ms is how long each step took, in milliseconds.
	Ms map[string]int64 `json:"ms,omitempty"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the result and keeps the JSON in Raw.
func (r *LabelResult) UnmarshalJSON(data []byte) (err error) {
	type plain LabelResult
	r.Raw, err = keepRaw(data, (*plain)(r))
	return err
}

// LabelDetails are the details read off a product's label. A field that was
// not found is its zero value: an empty string, a nil slice or a nil pointer.
type LabelDetails struct {
	// Headline is the largest text: brand and product name candidates.
	Headline    []string        `json:"headline,omitempty"`
	GTIN        string          `json:"gtin,omitempty"`
	GS1         *LabelGS1       `json:"gs1,omitempty"`
	NetQuantity *LabelQuantity  `json:"net_quantity,omitempty"`
	Alcohol     *LabelAlcohol   `json:"alcohol,omitempty"`
	AgeYears    *int            `json:"age_years,omitempty"`
	Vintage     *int            `json:"vintage,omitempty"`
	Bottled     *int            `json:"bottled,omitempty"`
	Dates       []LabelDate     `json:"dates,omitempty"`
	Lot         string          `json:"lot,omitempty"`
	Serial      string          `json:"serial,omitempty"`
	Model       string          `json:"model,omitempty"`
	Ingredients string          `json:"ingredients,omitempty"`
	Allergens   []string        `json:"allergens,omitempty"`
	MayContain  []string        `json:"may_contain,omitempty"`
	Nutrition   *LabelNutrition `json:"nutrition,omitempty"`
	Materials   []LabelMaterial `json:"materials,omitempty"`
	Origin      string          `json:"origin,omitempty"`
	Electrical  *LabelElectric  `json:"electrical,omitempty"`
	// Claims are among "organic", "vegan", "vegetarian", "gluten_free",
	// "lactose_free", "no_added_sugar", "sugar_free", "non_gmo",
	// "fairtrade", "kosher", "halal".
	Claims []string `json:"claims,omitempty"`
}

// LabelGS1 is the content of a GS1 code (GS1-128, GS1 DataMatrix).
type LabelGS1 struct {
	GTIN        string   `json:"gtin,omitempty"`
	Batch       string   `json:"batch,omitempty"`
	Serial      string   `json:"serial,omitempty"`
	Expiry      string   `json:"expiry,omitempty"`
	BestBefore  string   `json:"best_before,omitempty"`
	Production  string   `json:"production,omitempty"`
	Packed      string   `json:"packed,omitempty"`
	NetWeightKg *float64 `json:"net_weight_kg,omitempty"`
	Count       *int     `json:"count,omitempty"`
}

// LabelQuantity is a net quantity, such as "70 cl" or "6 × 330 ml".
type LabelQuantity struct {
	Value float64 `json:"value"`
	// Unit is "ml", "cl", "dl", "l", "fl_oz", "g", "kg", "oz" or "lb".
	Unit  string   `json:"unit"`
	Ml    *float64 `json:"ml,omitempty"`
	Grams *float64 `json:"grams,omitempty"`
	// Count is the number of items in a multipack.
	Count *int   `json:"count,omitempty"`
	Text  string `json:"text,omitempty"`
}

// LabelAlcohol is an alcohol statement.
type LabelAlcohol struct {
	// ABV is alcohol by volume, in percent.
	ABV  float64 `json:"abv"`
	Text string  `json:"text,omitempty"`
}

// LabelDate is a date printed on a label.
type LabelDate struct {
	// Kind is "best_before", "use_by", "expiry", "production" or "packed".
	Kind string `json:"kind"`
	// Date is an ISO date or month, when it could be read.
	Date string `json:"date,omitempty"`
	Text string `json:"text,omitempty"`
}

// LabelNutrition is a nutrition table; values not printed are nil.
type LabelNutrition struct {
	// Per is the reference amount, such as "100g" or "100ml".
	Per          string   `json:"per,omitempty"`
	EnergyKJ     *float64 `json:"energy_kj,omitempty"`
	EnergyKcal   *float64 `json:"energy_kcal,omitempty"`
	Fat          *float64 `json:"fat,omitempty"`
	Saturates    *float64 `json:"saturates,omitempty"`
	Carbohydrate *float64 `json:"carbohydrate,omitempty"`
	Sugars       *float64 `json:"sugars,omitempty"`
	Fibre        *float64 `json:"fibre,omitempty"`
	Protein      *float64 `json:"protein,omitempty"`
	Salt         *float64 `json:"salt,omitempty"`
}

// LabelMaterial is one material of a textile composition.
type LabelMaterial struct {
	Name    string   `json:"name"`
	Percent *float64 `json:"percent,omitempty"`
}

// LabelElectric is the electrical rating of a device.
type LabelElectric struct {
	Voltage    string   `json:"voltage,omitempty"`
	Frequency  string   `json:"frequency,omitempty"`
	PowerW     *float64 `json:"power_w,omitempty"`
	BatteryMAh *float64 `json:"battery_mah,omitempty"`
}

// IdentifyResult is what Identify returns.
type IdentifyResult struct {
	Catalogue string `json:"catalogue"`
	// Candidates are the products that match, best first.
	Candidates []Candidate `json:"candidates"`
	// Label is what was read off the label.
	Label IdentifyLabel `json:"label"`
	// Barcodes are the codes read.
	Barcodes []string `json:"barcodes"`
	// Text is all the text recognised.
	Text string `json:"text,omitempty"`
	// Ms is how long each step took, in milliseconds.
	Ms map[string]int64 `json:"ms,omitempty"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the result and keeps the JSON in Raw.
func (r *IdentifyResult) UnmarshalJSON(data []byte) (err error) {
	type plain IdentifyResult
	r.Raw, err = keepRaw(data, (*plain)(r))
	return err
}

// Candidate is a catalogue product that may be the one in the photos.
type Candidate struct {
	ProductID string  `json:"product_id"`
	Title     string  `json:"title"`
	Score     float64 `json:"score"`
	// Evidence says what agreed beyond the name: "barcode", "barcode_text",
	// "sku", "brand", and by the catalogue's kind "quantity", "pack", "spf"
	// (food, cosmetics), "strength", "count" (pharmacy, household), "spec",
	// "model" (electronics), "cask_number", "age", "age_weak", "vintage",
	// "abv", "size" (wine and spirits).
	Evidence []string `json:"evidence"`
}

// IdentifyLabel is what Identify read off the label; which values there
// are depends on the catalogue's kind.
type IdentifyLabel struct {
	Ages     []int     `json:"ages,omitempty"`
	WeakAges []int     `json:"weak_ages,omitempty"`
	Years    []int     `json:"years,omitempty"`
	ABVs     []float64 `json:"abvs,omitempty"`
	// Sizes are in centilitres.
	Sizes []float64 `json:"sizes,omitempty"`
	// Quantities are net quantities, in "g" or "ml".
	Quantities []Measure `json:"quantities,omitempty"`
	// Packs are the units in a multipack: 6 for "6 x 330 ml".
	Packs []int `json:"packs,omitempty"`
	// SPFs are sun protection factors.
	SPFs []int `json:"spfs,omitempty"`
	// Strengths are those of a medicine or supplement, in "mg", "iu" or
	// "percent".
	Strengths []Measure `json:"strengths,omitempty"`
	// Counts are the tablets, capsules, washes, rolls, wipes, batteries and
	// the like in the pack.
	Counts []int `json:"counts,omitempty"`
	// Specs are technical values, in "gb", "mah", "wh", "w", "v", "a", "hz"
	// or "in".
	Specs []Measure `json:"specs,omitempty"`
}

// Measure is a value read off a label, with its unit.
type Measure struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Point is a position in an image in pixels, from its top-left corner. In
// JSON it is an [x, y] pair.
type Point struct {
	X, Y float64
}

// MarshalJSON encodes the point as [x, y].
func (p Point) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]float64{p.X, p.Y})
}

// UnmarshalJSON decodes the point from [x, y].
func (p *Point) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var pair []float64
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) < 2 {
		return fmt.Errorf("photos: a point is [x, y], got %d numbers", len(pair))
	}
	p.X, p.Y = pair[0], pair[1]
	return nil
}

// PaletteResult is what Palette returns.
type PaletteResult struct {
	// Width and Height are the image's size in pixels.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Colors are the image's main colours, the most common first.
	Colors []PaletteColor `json:"colors"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the result and keeps the JSON in Raw.
func (r *PaletteResult) UnmarshalJSON(data []byte) (err error) {
	type plain PaletteResult
	r.Raw, err = keepRaw(data, (*plain)(r))
	return err
}

// PaletteColor is one colour of a palette.
type PaletteColor struct {
	// Hex is the colour as "#rrggbb".
	Hex string `json:"hex"`
	// RGB is its red, green and blue, each 0–255.
	RGB []int `json:"rgb"`
	// Share is the fraction of the visible image this colour stands for,
	// 0–1.
	Share float64 `json:"share"`
}

// FacesResult is what FindFaces returns.
type FacesResult struct {
	// Width and Height are the photo's size in pixels, after its EXIF
	// rotation.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Faces are the faces found, the surest first; empty when there are
	// none.
	Faces []Face `json:"faces"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the result and keeps the JSON in Raw.
func (r *FacesResult) UnmarshalJSON(data []byte) (err error) {
	type plain FacesResult
	r.Raw, err = keepRaw(data, (*plain)(r))
	return err
}

// Face is one face in a photo. Its coordinates are in the photo's pixels,
// after its EXIF rotation.
type Face struct {
	// Box runs from forehead to chin.
	Box FaceBox `json:"box"`
	// Landmarks are five points of the face.
	Landmarks FaceLandmarks `json:"landmarks"`
	// Score is how sure the model is that this is a face, 0–1.
	Score float64 `json:"score"`
	// Roll is the tilt of the line through the eyes, in degrees: 0 for a
	// level head.
	Roll float64 `json:"roll"`
}

// FaceBox is where a face is. It can reach past the photo's edges (a
// negative X or Y) for a face partly out of the frame.
type FaceBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// FaceLandmarks are five points of a face. "Right" and "left" are the
// person's own, so RightEye is the one on the left of the photo.
type FaceLandmarks struct {
	RightEye   Point `json:"right_eye"`
	LeftEye    Point `json:"left_eye"`
	Nose       Point `json:"nose"`
	MouthRight Point `json:"mouth_right"`
	MouthLeft  Point `json:"mouth_left"`
}

// CaptionResult is what Caption returns.
type CaptionResult struct {
	// Caption is the description, in English. It can be wrong: read it
	// before publishing it.
	Caption string `json:"caption"`
	// Detail is how much was asked for: "short", "detailed" or "paragraph".
	Detail string `json:"detail"`
	// Width and Height are the photo's size in pixels.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the result and keeps the JSON in Raw.
func (r *CaptionResult) UnmarshalJSON(data []byte) (err error) {
	type plain CaptionResult
	r.Raw, err = keepRaw(data, (*plain)(r))
	return err
}

// IDPhotoDocument is an identity document whose photo rules IDPhoto knows.
type IDPhotoDocument struct {
	// Name is what IDPhotoOptions.Document takes, such as "us-passport".
	Name  string `json:"name"`
	Title string `json:"title"`
	// Checked is the date (YYYY-MM-DD) the rules were last compared with
	// the sources.
	Checked string `json:"checked"`
	// Sources are the official pages the rules were read from.
	Sources []string `json:"sources"`
	// Notes are rules the tool cannot check or make true.
	Notes []string `json:"notes"`
	// WidthMm and HeightMm are the photo's size in millimetres.
	WidthMm  float64 `json:"width_mm"`
	HeightMm float64 `json:"height_mm"`
	// HeadMinMm and HeadMaxMm are the smallest and largest allowed head,
	// chin to crown, in millimetres.
	HeadMinMm float64 `json:"head_min_mm"`
	HeadMaxMm float64 `json:"head_max_mm"`
	// EyesFromBottomMm is the smallest and the largest distance of the eyes
	// from the bottom edge, for a document with such a rule; nil otherwise.
	EyesFromBottomMm []float64 `json:"eyes_from_bottom_mm,omitempty"`
	// Background is the background the rules ask for, as a colour.
	Background string `json:"background"`
	// Alteration is what the authority says about photos changed by
	// software, in its own words; empty when it says nothing.
	Alteration string `json:"alteration,omitempty"`
	// Raw is the document as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the document and keeps the JSON in Raw.
func (d *IDPhotoDocument) UnmarshalJSON(data []byte) (err error) {
	type plain IDPhotoDocument
	d.Raw, err = keepRaw(data, (*plain)(d))
	return err
}

// Account is the caller's account.
type Account struct {
	Account string `json:"account,omitempty"`
	// Key is the key's id, "browser" or "job".
	Key string `json:"key,omitempty"`
	// Credits is the balance, nil for an unlimited account.
	Credits *int64 `json:"credits,omitempty"`
	// Prices are the credits one image costs, by operation.
	Prices map[string]int64 `json:"prices,omitempty"`
	// Unlimited is true for a server key, which is not metered.
	Unlimited bool `json:"unlimited,omitempty"`
	// Raw is the response as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the account and keeps the JSON in Raw.
func (a *Account) UnmarshalJSON(data []byte) (err error) {
	type plain Account
	a.Raw, err = keepRaw(data, (*plain)(a))
	return err
}

// Preset is a built-in preset for Run.
type Preset struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Marketplace is a marketplace's image rules.
type Marketplace struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	// Checked is the date (YYYY-MM-DD) the rules were last checked against
	// the sources.
	Checked string   `json:"checked"`
	Sources []string `json:"sources"`
	// Notes are rules that cannot be checked automatically.
	Notes []string `json:"notes,omitempty"`
	// Rules are the rules as the API gives them; each has at least "check"
	// and "severity".
	Rules []map[string]any `json:"rules"`
	// Fix is what Fix produces for this marketplace.
	Fix map[string]any `json:"fix"`
	// Raw is the marketplace as it came.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the marketplace and keeps the JSON in Raw.
func (m *Marketplace) UnmarshalJSON(data []byte) (err error) {
	type plain Marketplace
	m.Raw, err = keepRaw(data, (*plain)(m))
	return err
}

// WebhookSecret is the key job webhooks are signed with.
type WebhookSecret struct {
	// Secret is the signing key, for VerifyWebhook.
	Secret string `json:"secret"`
	// Header is the name of the signature header, "Photos-Signature".
	Header string `json:"header"`
	// Scheme describes how the signature is made.
	Scheme string `json:"scheme"`
}
