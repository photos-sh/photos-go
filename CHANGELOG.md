# Changelog

All notable changes to this library are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the library uses
[semantic versioning](https://semver.org/).

## 1.0.0 (unreleased)

First version.

- `identify` matches against the organisation's own catalogues (uploaded in
  the console; turned on per organisation). Without it the API answers
  `404 not_found` (`ErrNotFound`). A catalogue is one of seven kinds of
  product, and the evidence and the label's values follow it: `sku`, `brand`,
  `quantity`, `pack`, `spf`, `strength`, `count`, `spec`, `model` beside the
  wine and spirits ones.
- Client for the Photos.sh HTTP API: remove background, resize, upscale,
  erase, remove text, run (presets and recipes), fix, check, read barcodes,
  read text, read label, identify, account, presets, marketplaces, health and
  the webhook secret.
- Batch jobs: create (URLs or uploads), get, list, delete, item results, wait.
- Job item results of `fix` and `remove-text` jobs as typed results
  (`JobItemResult.Fix`, `JobItemResult.RemoveText`), with
  `ErrMissingResultHeaders` when the result does not carry their headers.
- Streaming multipart uploads from files, bytes and readers.
- Retries for `503` and for connection failures, honouring `Retry-After`.
- Typed errors with `errors.Is` sentinels and `errors.As` access.
- Webhook signature verification.
- The editing tools the API added on 2026-11-12: `Enhance`, `Crop`,
  `Watermark` (text or a logo), `Expand`, `Vectorize` (SVG, with its `Paths`),
  `Palette`, `FindFaces`, `BlurFaces`, `IDPhoto` and `IDPhotos` (the built-in
  documents), `Denoise`, `Colorize`, `Select`, `RestoreFaces` and `Caption`.
- `RemoveBackground` stages the cutout: `Width`, `Height`, `Fill`, `Align`,
  `Shadow`, `Reflection`, `Blur` and a `BackgroundImage`.
- `Resize` takes `Trim`, `TrimTolerance` and `DPI`.
- `Image.SubjectFound` (the `X-Subject-Found` header of `Crop` and
  `RemoveBackground`): nil when the response does not say.
- Options whose zero is a value of its own are pointers, made with
  `photos.Int`, `photos.Float` and `photos.Bool`.
- Job operations for the new tools, and the item results of
  `remove-background`, `crop`, `vectorize`, `blur-faces`, `id-photo`, `select`
  and `restore-faces` jobs as typed results (`JobItemResult.RemoveBackground`,
  `Crop`, `Vectorize`, `BlurFaces`, `IDPhoto`, `Select`, `RestoreFaces`).
- A job parameter that repeats (the `point` of a `select` job) takes a
  `[]string`; such parameters are sent as a query string, with URL inputs too.
