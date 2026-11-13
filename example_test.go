package photos_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	photos "github.com/photos-sh/photos-go"
)

func Example() {
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

func ExampleNew() {
	client, err := photos.New("your-api-key",
		photos.WithBaseURL("http://localhost:8080"), // a local engine
		photos.WithTimeout(60*time.Second),
		photos.WithRetries(4),
		photos.WithUserAgent("my-shop/1.2"),
		photos.WithHTTPClient(&http.Client{}),
	)
	if err != nil {
		log.Fatal(err)
	}
	status, err := client.Health(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(status)
}

func ExampleClient_RemoveBackground() {
	client, _ := photos.New("")
	img, err := client.RemoveBackground(context.Background(), photos.File("shoe.jpg"),
		&photos.RemoveBackgroundOptions{
			Background: "white",
			Crop:       true,
			Padding:    40,
			Format:     photos.FormatJPEG,
			Quality:    90,
		})
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("shoe-white" + img.Extension()); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Resize() {
	client, _ := photos.New("")

	// Any io.Reader will do; here, standard input.
	img, err := client.Resize(context.Background(), photos.Reader("photo.jpg", os.Stdin),
		&photos.ResizeOptions{Width: 1600, Format: photos.FormatWebP, MaxBytes: 300_000})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d×%d at quality %d, %d bytes\n", img.Width, img.Height, img.Quality, len(img.Bytes))
}

func ExampleClient_Upscale() {
	client, _ := photos.New("")
	img, err := client.Upscale(context.Background(), photos.File("small.jpg"), &photos.UpscaleOptions{Scale: 2})
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("large.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Erase() {
	client, _ := photos.New("")
	img, err := client.Erase(context.Background(), photos.File("room.jpg"), &photos.EraseOptions{
		Rects: []photos.Rect{{X: 40, Y: 60, Width: 200, Height: 120}},
		// And/or a mask the size of the photo, white where to remove:
		Mask: photos.File("room-mask.png"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("room-clean.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_RemoveText() {
	client, _ := photos.New("")
	res, err := client.RemoveText(context.Background(), photos.File("listing.jpg"),
		&photos.RemoveTextOptions{Matching: `sale|€|www\.`})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("removed %d lines, kept %d\n", res.Removed, res.Kept)
	if err := res.Save("listing-clean.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Run() {
	client, _ := photos.New("")
	ctx := context.Background()

	// A built-in preset (see Presets)…
	img, err := client.Run(ctx, photos.File("shoe.jpg"), &photos.RunOptions{Preset: "marketplace"})
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("shoe-marketplace.jpg"); err != nil {
		log.Fatal(err)
	}

	// …or a recipe of your own, as TOML.
	recipe, err := os.ReadFile("recipe.toml")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := client.Run(ctx, photos.File("shoe.jpg"), &photos.RunOptions{Recipe: string(recipe)}); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Fix() {
	client, _ := photos.New("")
	res, err := client.Fix(context.Background(), photos.File("shoe.jpg"), "amazon")
	if err != nil {
		log.Fatal(err)
	}
	if !res.Passed {
		fmt.Println("still failing:", res.Findings)
	}
	if err := res.Save("shoe-amazon.jpg"); err != nil {
		log.Fatal(err)
	}
}

// A cutout staged in the same call: on a canvas, standing on a soft shadow,
// in front of a backdrop of your own.
func ExampleClient_RemoveBackground_staged() {
	client, _ := photos.New("")
	img, err := client.RemoveBackground(context.Background(), photos.File("shoe.jpg"),
		&photos.RemoveBackgroundOptions{
			Width:           1600,
			Height:          1600,
			Fill:            0.8,
			Align:           "bottom",
			Shadow:          "contact",
			BackgroundImage: photos.File("studio.jpg"),
			Format:          photos.FormatJPEG,
		})
	if err != nil {
		log.Fatal(err)
	}
	if img.SubjectFound != nil && !*img.SubjectFound {
		fmt.Println("no subject in the photo: the canvas is empty")
	}
	if err := img.Save("shoe-staged.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Enhance() {
	client, _ := photos.New("")
	ctx := context.Background()

	// No options: automatic levels and white balance.
	img, err := client.Enhance(ctx, photos.File("dull.jpg"), nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("bright.jpg"); err != nil {
		log.Fatal(err)
	}

	// Settings of your own, beside the automatic part.
	_, err = client.Enhance(ctx, photos.File("dull.jpg"), &photos.EnhanceOptions{
		Auto:       photos.Bool(true),
		Brightness: 10,
		Saturation: -15,
		Sharpen:    30,
	})
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Crop() {
	client, _ := photos.New("")
	ctx := context.Background()

	img, err := client.Crop(ctx, photos.File("shoe.jpg"), &photos.CropOptions{Aspect: "4:5"})
	if err != nil {
		log.Fatal(err)
	}
	if img.SubjectFound != nil && !*img.SubjectFound {
		fmt.Println("no subject found: the crop is centred")
	}

	// An avatar: tight around the face, with room for hair and chin.
	avatar, err := client.Crop(ctx, photos.File("portrait.jpg"), &photos.CropOptions{
		Focus:   "face",
		Zoom:    true,
		Padding: photos.Float(0.6),
		Width:   400,
		Height:  400,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := avatar.Save("avatar.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Watermark() {
	client, _ := photos.New("")
	ctx := context.Background()

	// Text…
	img, err := client.Watermark(ctx, photos.File("photo.jpg"), &photos.WatermarkOptions{
		Text:     "© Acme",
		Position: "bottom-right",
		Opacity:  60,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("photo-marked.jpg"); err != nil {
		log.Fatal(err)
	}

	// …or a logo, here tiled across the photo.
	_, err = client.Watermark(ctx, photos.File("photo.jpg"), &photos.WatermarkOptions{
		Watermark: photos.File("logo.png"),
		Scale:     15,
		Tile:      true,
	})
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Expand() {
	client, _ := photos.New("")
	ctx := context.Background()

	// To a shape; the new area continues the backdrop.
	img, err := client.Expand(ctx, photos.File("shoe.jpg"), &photos.ExpandOptions{Aspect: "16:9"})
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("shoe-wide.jpg"); err != nil {
		log.Fatal(err)
	}

	// By pixels, with a flat colour.
	_, err = client.Expand(ctx, photos.File("shoe.jpg"), &photos.ExpandOptions{Left: 200, Right: 200, Fill: "white"})
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Vectorize() {
	client, _ := photos.New("")
	res, err := client.Vectorize(context.Background(), photos.File("logo.png"), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Paths, "paths, traced at", res.Width, "×", res.Height)
	if err := res.Save("logo.svg"); err != nil { // res.Bytes is the SVG's text
		log.Fatal(err)
	}
}

func ExampleClient_BlurFaces() {
	client, _ := photos.New("")
	res, err := client.BlurFaces(context.Background(), photos.File("street.jpg"),
		&photos.BlurFacesOptions{Mode: "pixelate"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Faces, "faces hidden") // check it: a face not found still shows
	if err := res.Save("street-anonymous.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_IDPhoto() {
	client, _ := photos.New("")
	res, err := client.IDPhoto(context.Background(), photos.File("portrait.jpg"),
		&photos.IDPhotoOptions{Document: "us-passport"})
	if errors.Is(err, photos.ErrUnprocessable) {
		log.Fatal("the photo must show one face") // none, or more than one
	} else if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("head %.1f mm; findings: %v\n", res.HeadMm, res.Findings)
	if !res.Passed {
		fmt.Println("the tool saw something wrong; an empty list is still not an approval")
	}
	if err := res.Save("passport.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_IDPhotos() {
	client, _ := photos.New("")
	documents, err := client.IDPhotos(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range documents {
		fmt.Printf("%s: %s, %g × %g mm, head %g–%g mm\n", d.Name, d.Title, d.WidthMm, d.HeightMm, d.HeadMinMm, d.HeadMaxMm)
		if d.Alteration != "" {
			fmt.Println(" ", d.Alteration)
		}
	}
}

func ExampleClient_Denoise() {
	client, _ := photos.New("")
	img, err := client.Denoise(context.Background(), photos.File("grainy.jpg"), &photos.DenoiseOptions{Strength: 80})
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("clean.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Colorize() {
	client, _ := photos.New("")
	img, err := client.Colorize(context.Background(), photos.File("grandparents.jpg"), nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := img.Save("grandparents-colour.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Select() {
	client, _ := photos.New("")
	ctx := context.Background()

	// The object at a click, cut out.
	res, err := client.Select(ctx, photos.File("desk.jpg"), &photos.SelectOptions{
		Points:  []photos.Point{{X: 420, Y: 310}},
		Exclude: []photos.Point{{X: 600, Y: 300}},
		Crop:    true,
	})
	if errors.Is(err, photos.ErrUnprocessable) {
		log.Fatal("nothing was found at that point")
	} else if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("score %.2f\n", res.Score)
	if err := res.Save("mug.png"); err != nil {
		log.Fatal(err)
	}

	// Its mask is what Erase takes to remove it.
	mask, err := client.Select(ctx, photos.File("desk.jpg"), &photos.SelectOptions{
		Points: []photos.Point{{X: 420, Y: 310}},
		Mask:   true,
	})
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.Erase(ctx, photos.File("desk.jpg"), &photos.EraseOptions{
		Mask: photos.Bytes("mask.png", mask.Bytes),
	})
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_RestoreFaces() {
	client, _ := photos.New("")
	res, err := client.RestoreFaces(context.Background(), photos.File("old-family.jpg"), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Faces, "faces restored,", res.FacesSkipped, "left alone")
	if err := res.Save("old-family-restored.jpg"); err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Check() {
	client, _ := photos.New("")
	res, err := client.Check(context.Background(), photos.File("shoe.jpg"),
		&photos.CheckOptions{Marketplace: "amazon"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d×%d, sharpness %.2f\n", res.Metrics.Width, res.Metrics.Height, res.Metrics.Sharpness)
	for _, f := range res.Findings {
		fmt.Println(f.Severity, f.Code, f.Message)
	}
	if v := res.Marketplace; v != nil && !v.Passed {
		for _, f := range v.Findings {
			fmt.Println("amazon:", f.Code, f.Message)
		}
	}
}

func ExampleClient_ReadBarcodes() {
	client, _ := photos.New("")
	res, err := client.ReadBarcodes(context.Background(), photos.File("box.jpg"),
		&photos.ReadBarcodesOptions{AllFormats: true})
	if err != nil {
		log.Fatal(err)
	}
	for _, b := range res.Barcodes {
		fmt.Println(b.Format, b.Value)
	}
}

func ExampleClient_ReadText() {
	client, _ := photos.New("")
	res, err := client.ReadText(context.Background(), photos.File("label.jpg"))
	if err != nil {
		log.Fatal(err)
	}
	for _, line := range res.Lines {
		fmt.Printf("%.2f %s\n", line.Score, line.Text)
	}
}

func ExampleClient_ReadLabel() {
	client, _ := photos.New("")
	res, err := client.ReadLabel(context.Background(), []photos.Input{
		photos.File("front.jpg"),
		photos.File("back.jpg"),
	})
	if err != nil {
		log.Fatal(err)
	}
	label := res.Label
	fmt.Println(label.Headline, label.GTIN)
	if q := label.NetQuantity; q != nil {
		fmt.Println(q.Value, q.Unit)
	}
	if label.Alcohol != nil {
		fmt.Printf("%.1f%% vol\n", label.Alcohol.ABV)
	}
}

func ExampleClient_Identify() {
	client, _ := photos.New("")
	res, err := client.Identify(context.Background(),
		[]photos.Input{photos.File("front.jpg"), photos.File("back.jpg")},
		&photos.IdentifyOptions{Catalogue: "shoes", Top: 3})
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range res.Candidates {
		fmt.Println(c.ProductID, c.Title, c.Score, c.Evidence)
	}
}

func ExampleClient_Palette() {
	client, _ := photos.New("")
	res, err := client.Palette(context.Background(), photos.File("shoe.png"), &photos.PaletteOptions{Colors: 3})
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range res.Colors {
		fmt.Printf("%s %.0f%%\n", c.Hex, c.Share*100)
	}
}

func ExampleClient_FindFaces() {
	client, _ := photos.New("")
	res, err := client.FindFaces(context.Background(), photos.File("group.jpg"), nil)
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range res.Faces {
		fmt.Printf("%.0f×%.0f at %.0f,%.0f, score %.2f, tilted %.1f°\n",
			f.Box.Width, f.Box.Height, f.Box.X, f.Box.Y, f.Score, f.Roll)
		fmt.Println("  nose at", f.Landmarks.Nose.X, f.Landmarks.Nose.Y)
	}
}

func ExampleClient_Caption() {
	client, _ := photos.New("")
	res, err := client.Caption(context.Background(), photos.File("shoe.jpg"), &photos.CaptionOptions{Detail: "detailed"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Caption) // a draft, in English: read it before publishing it
}

func ExampleClient_Account() {
	client, _ := photos.New("")
	acc, err := client.Account(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if acc.Unlimited {
		fmt.Println(acc.Account, "is not metered")
	} else if acc.Credits != nil {
		fmt.Println(*acc.Credits, "credits left; a background removal costs", acc.Prices["remove-background"])
	}
}

func ExampleClient_Presets() {
	client, _ := photos.New("")
	presets, err := client.Presets(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range presets {
		fmt.Println(p.Name, "-", p.Description)
	}
}

func ExampleClient_Marketplaces() {
	client, _ := photos.New("")
	marketplaces, err := client.Marketplaces(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range marketplaces {
		fmt.Println(m.Name, m.Title, "checked", m.Checked)
		for _, rule := range m.Rules {
			fmt.Println(" ", rule["check"], rule["severity"])
		}
	}
}

func ExampleClient_Health() {
	client, _ := photos.New("")
	status, err := client.Health(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if status != photos.HealthOK {
		fmt.Println("not ready:", status)
	}
}

// Errors from the API are *photos.Error values: branch on the code with
// errors.Is, or read the details with errors.As.
func ExampleError() {
	client, _ := photos.New("")
	_, err := client.Upscale(context.Background(), photos.File("small.jpg"), nil)

	var apiErr *photos.Error
	var connErr *photos.ConnectionError
	switch {
	case err == nil:
		// Use the image.
	case errors.Is(err, photos.ErrPaymentRequired):
		fmt.Println("out of credits")
	case errors.Is(err, photos.ErrBusy):
		// Already retried; apiErr.RetryAfter says when to try again.
		fmt.Println("busy")
	case errors.As(err, &apiErr):
		fmt.Println(apiErr.Status, apiErr.Code, apiErr.Message)
	case errors.As(err, &connErr):
		fmt.Println("could not reach the API; timed out:", connErr.Timeout)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		fmt.Println("the context ended")
	default:
		fmt.Println(err) // a file that could not be read, for one
	}
}

func ExampleJobsService_Create() {
	client, _ := photos.New("")
	ctx := context.Background()

	// Images the API fetches…
	job, err := client.Jobs.Create(ctx, &photos.JobRequest{
		Operation:  photos.OperationRemoveBackground,
		Params:     map[string]any{"format": "png", "crop": true},
		URLs:       []string{"https://example.com/photos/1.jpg", "https://example.com/photos/2.jpg"},
		WebhookURL: "https://example.com/hooks/photos",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(job.ID, job.Status)

	// …or images you upload.
	job, err = client.Jobs.Create(ctx, &photos.JobRequest{
		Operation: photos.OperationRun,
		Params:    map[string]any{"preset": "marketplace"},
		Images:    []photos.Input{photos.File("1.jpg"), photos.File("2.jpg")},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(job.ID, job.Counts.Total)
}

func ExampleJobsService_Wait() {
	client, _ := photos.New("")
	ctx := context.Background()

	job, err := client.Jobs.Wait(ctx, "job_5f0c", &photos.WaitOptions{
		Interval: 5 * time.Second,
		Timeout:  30 * time.Minute,
	})
	if errors.Is(err, photos.ErrWaitTimeout) {
		log.Fatalf("still %s: %d of %d done", job.Status, job.Counts.Done, job.Counts.Total)
	} else if err != nil {
		log.Fatal(err)
	}

	for n, item := range job.Items {
		if item.Status != photos.ItemDone {
			fmt.Println(item.Input, "failed:", item.Error)
			continue
		}
		res, err := client.Jobs.Item(ctx, job.ID, n)
		if err != nil {
			log.Fatal(err)
		}
		if img := res.Image(); img != nil {
			if err := img.Save(fmt.Sprintf("result-%d%s", n, img.Extension())); err != nil {
				log.Fatal(err)
			}
		}
	}
}

func ExampleJobsService_Item() {
	client, _ := photos.New("")

	// The result of an "ocr" job's first item is JSON.
	res, err := client.Jobs.Item(context.Background(), "job_5f0c", 0)
	if err != nil {
		log.Fatal(err)
	}
	var text photos.TextResult
	if err := res.JSON(&text); err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(text.Lines), "lines")
}

// The items of a "fix" job carry the marketplace's verdict, as Client.Fix
// returns it; those of a "remove-text" job have RemoveText likewise.
func ExampleJobItemResult_Fix() {
	client, _ := photos.New("")
	ctx := context.Background()

	job, err := client.Jobs.Create(ctx, &photos.JobRequest{
		Operation: photos.OperationFix,
		Params:    map[string]any{"marketplace": "etsy"},
		Images:    []photos.Input{photos.File("1.jpg"), photos.File("2.jpg")},
	})
	if err != nil {
		log.Fatal(err)
	}
	if job, err = client.Jobs.Wait(ctx, job.ID, nil); err != nil {
		log.Fatal(err)
	}
	for n, item := range job.Items {
		if item.Status != photos.ItemDone {
			continue
		}
		res, err := client.Jobs.Item(ctx, job.ID, n)
		if err != nil {
			log.Fatal(err)
		}
		fix, err := res.Fix()
		if errors.Is(err, photos.ErrMissingResultHeaders) {
			log.Fatal("this engine does not keep the verdict on job items")
		} else if err != nil {
			log.Fatal(err)
		}
		fmt.Println(item.Input, fix.Passed, fix.Findings)
		if err := fix.Save(fmt.Sprintf("fixed-%d%s", n, fix.Extension())); err != nil {
			log.Fatal(err)
		}
	}
}

func ExampleJobsService_List() {
	client, _ := photos.New("")
	ctx := context.Background()

	jobs, err := client.Jobs.List(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, j := range jobs {
		fmt.Println(j.ID, j.Operation, j.Status, j.Counts.Done, "/", j.Counts.Total)
		if j.Status == photos.JobCompleted {
			if err := client.Jobs.Delete(ctx, j.ID); err != nil {
				log.Fatal(err)
			}
		}
	}
}

// A webhook endpoint: verify the delivery against the raw body before
// trusting it.
func ExampleVerifyWebhook() {
	secret := os.Getenv("PHOTOS_WEBHOOK_SECRET") // from Client.WebhookSecret

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
}

func ExampleVerifyWebhookAt() {
	body := []byte(`{"type":"job.completed","job":{"id":"job_1"}}`)
	header := "t=1800000000,v1=90c1672761ea299dc1569349a086c9d9c3053d8fa0636d77d40a0286ce3fb226"

	event, err := photos.VerifyWebhookAt(body, header, "whsec_test", 5*time.Minute, time.Unix(1800000060, 0))
	fmt.Println(event.Type, event.Job.ID, err)

	_, err = photos.VerifyWebhookAt(body, header, "whsec_test", 5*time.Minute, time.Unix(1800009999, 0))
	fmt.Println(errors.Is(err, photos.ErrWebhookTimestamp))

	_, err = photos.VerifyWebhookAt(body, header, "another secret", 5*time.Minute, time.Unix(1800000060, 0))
	fmt.Println(err)

	// Output:
	// job.completed job_1 <nil>
	// true
	// photos: webhook: signature does not match
}
