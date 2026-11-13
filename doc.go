// Package photos is the client library for the Photos.sh HTTP API: image
// tools for product photos. It removes backgrounds, resizes and compresses,
// upscales, erases objects and text, checks photos against marketplace rules
// and fixes them, and reads barcodes, text and label details. It also
// enhances, crops, watermarks, expands, vectorizes, denoises and colorizes
// photos, selects an object at a point, finds, hides and restores faces,
// frames identity photos, and lists colours and writes captions.
//
// Make a client with your API key (or leave it empty to use the
// PHOTOS_API_KEY environment variable) and call a method with an image:
//
//	client, err := photos.New("")
//	if err != nil {
//		log.Fatal(err)
//	}
//	img, err := client.RemoveBackground(ctx, photos.File("shoe.jpg"), nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	err = img.Save("shoe.png")
//
// # Images
//
// An image argument is an Input: File for a path, Bytes for data in memory,
// Reader for a stream. Uploads are streamed. Methods that change a photo
// return an *Image (the bytes, the content type, the size), or a result
// that holds one beside what the method reports, such as the faces hidden;
// methods that read a photo return a typed result.
//
// # Options
//
// Each method takes a pointer to its own options struct, which may be nil.
// A field left at its zero value is not sent, and the API then uses its own
// default. The few options whose zero is a value of its own (a margin of 0,
// an automatic correction turned off) are pointers: nil is not sent, and
// Int, Float and Bool make one, as in Margin: photos.Float(0).
//
// # Errors
//
// An error response from the API is an *Error with the HTTP status and the
// API's code and message. Test for a code with errors.Is and a sentinel
// (ErrUnauthorized, ErrPaymentRequired, ErrBusy…), or get the *Error with
// errors.As. A failure to reach the API is a *ConnectionError. A cancelled
// context or a passed context deadline comes back as context.Canceled or
// context.DeadlineExceeded.
//
// # Retries and timeouts
//
// A request that gets a 503 "busy" (or a 503 that is not the API's own, as
// from a proxy) is sent again, twice by default, after the Retry-After the
// API gives (at most 30 s) or 1 s then 2 s; so is one that fails to connect,
// except Jobs.Create. Nothing else is retried: not a 503
// "model_unavailable", which is permanent, and not a timeout. Each attempt may take 120 s by default. See WithRetries and
// WithTimeout, and Input for how retries treat readers.
//
// # Jobs and webhooks
//
// Client.Jobs runs one operation on many images in the background: Create,
// then Wait, or give a webhook URL and check each delivery with
// VerifyWebhook.
package photos
