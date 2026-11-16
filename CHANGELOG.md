# Changelog

All notable changes to this library are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the library uses
[semantic versioning](https://semver.org/).

## Unreleased

### Fixed

- `Version` (sent in the User-Agent) says the library's version: it said
  1.0.0 in 1.0.0 to 1.2.0.

## 1.2.0 (2026-11-16)

### Added

- `Image.Barcodes`, `Image.Text` and `Image.TextTruncated`: what a recipe's
  `[barcodes]` and `[text]` steps read off the photo, from the `X-Barcodes`,
  `X-Text` and `X-Text-Truncated` headers (also on job items). Nil when the
  recipe has no such step, empty when it found nothing.
- `Client.Preset(ctx, name)`: a built-in preset's recipe as TOML
  (`GET /v1/presets/{name}`), to start a recipe of your own from.

## 1.1.0 (2026-11-16)

### Added

- `RemoveBackgroundOptions.Studio` (the `studio` parameter, engine 0.1.10):
  for studio shots, a product on a plain, light backdrop. It also keeps what
  clearly stands out from the backdrop and touches the product, such as the
  gift box, case or lid shown with a bottle. Shadows and the floor are left
  out, and a photo without a plain light backdrop is cut out as without it.

### Changed

- The documentation of `RemoveBackground` and `Image.SubjectFound`: with no
  subject in the photo (engine 0.1.9), the API returns the photo as it was,
  with or without a canvas, instead of an empty canvas. `SubjectFound` is
  then false, as before.

## 1.0.0 (2026-11-13)

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
