package photos

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sync"
)

// Input is an image to send: make one with File, Bytes or Reader. The zero
// Input is "no image".
//
// The request body is streamed, not built in memory, so a large file costs
// no more memory than a small one.
//
// Retries need the body a second time. File and Bytes inputs can always be
// sent again. A Reader input can be sent again only if it is also an
// io.Seeker (an *os.File, a *bytes.Reader); a request with any other reader
// is sent once and never retried, whatever WithRetries says: the first
// answer, such as a 503 "busy" *Error, is returned as it is. Read the data
// into memory and use Bytes when you want such a stream retried.
type Input struct {
	kind   inputKind
	name   string
	path   string
	data   []byte
	reader *readerSource
}

type inputKind uint8

const (
	inputNone inputKind = iota
	inputFile
	inputBytes
	inputReader
)

// File is the image in the file at path. The file is opened when the request
// is sent, and again for each retry.
func File(path string) Input {
	return Input{kind: inputFile, path: path, name: filepath.Base(path)}
}

// Bytes is an image held in memory. name is the file name the API sees (job
// items are listed under it); it may be empty.
func Bytes(name string, b []byte) Input {
	return Input{kind: inputBytes, name: name, data: b}
}

// Reader is an image read from r, which is read to its end but not closed.
// name is the file name the API sees; it may be empty.
//
// A request reading from r is retried only if r is also an io.Seeker; see
// Input.
func Reader(name string, r io.Reader) Input {
	return Input{kind: inputReader, name: name, reader: &readerSource{r: r}}
}

// IsZero reports whether in is the zero Input: no image.
func (in Input) IsZero() bool { return in.kind == inputNone }

func (in Input) fileName() string {
	if in.name == "" || in.name == "." || in.name == string(filepath.Separator) {
		return "image"
	}
	return in.name
}

func (in Input) replayable() bool {
	if in.kind != inputReader {
		return true
	}
	if in.reader == nil || in.reader.r == nil {
		return true // fails before anything is sent
	}
	_, ok := in.reader.r.(io.Seeker)
	return ok
}

// validate reports what can be known to be wrong before sending.
func (in Input) validate() error {
	switch in.kind {
	case inputNone:
		return errors.New("photos: no image: use photos.File, photos.Bytes or photos.Reader")
	case inputFile:
		if in.path == "" {
			return errors.New("photos: File: empty path")
		}
	case inputReader:
		if in.reader == nil || in.reader.r == nil {
			return errors.New("photos: Reader: nil reader")
		}
	}
	return nil
}

// open returns the image's bytes for one attempt, and how many there are
// (-1 when that cannot be known without reading them).
func (in Input) open() (io.ReadCloser, int64, error) {
	if err := in.validate(); err != nil {
		return nil, 0, err
	}
	switch in.kind {
	case inputFile:
		info, err := os.Stat(in.path)
		if err != nil {
			return nil, 0, fmt.Errorf("photos: %w", err)
		}
		if info.IsDir() {
			return nil, 0, fmt.Errorf("photos: %s is a directory", in.path)
		}
		return &lazyFile{path: in.path}, info.Size(), nil
	case inputBytes:
		return io.NopCloser(bytes.NewReader(in.data)), int64(len(in.data)), nil
	default:
		return in.reader.open()
	}
}

// readerSource is a caller's reader, which can be read once unless it seeks.
type readerSource struct {
	mu    sync.Mutex
	r     io.Reader
	used  bool
	start int64
}

func (s *readerSource) open() (io.ReadCloser, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	size := int64(-1)
	if seeker, ok := s.r.(io.Seeker); ok {
		if !s.used {
			pos, err := seeker.Seek(0, io.SeekCurrent)
			if err != nil {
				return nil, 0, fmt.Errorf("photos: reading the input: %w", err)
			}
			s.start = pos
		}
		end, err := seeker.Seek(0, io.SeekEnd)
		if err == nil {
			size = end - s.start
		}
		if _, err := seeker.Seek(s.start, io.SeekStart); err != nil {
			return nil, 0, fmt.Errorf("photos: rewinding the input: %w", err)
		}
	} else if s.used {
		return nil, 0, errors.New("photos: the reader was already sent; only a seekable reader can be sent again")
	} else if l, ok := s.r.(interface{ Len() int }); ok {
		// *bytes.Buffer, *strings.Reader: what is left to read.
		size = int64(l.Len())
	}
	s.used = true
	return io.NopCloser(s.r), size, nil
}

// lazyFile opens its file at the first read, so a request with many files
// holds one open at a time.
type lazyFile struct {
	path   string
	mu     sync.Mutex
	f      *os.File
	closed bool
}

func (l *lazyFile) Read(p []byte) (int, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return 0, os.ErrClosed
	}
	if l.f == nil {
		f, err := os.Open(l.path)
		if err != nil {
			l.mu.Unlock()
			return 0, err
		}
		l.f = f
	}
	f := l.f
	l.mu.Unlock()

	n, err := f.Read(p)
	if err == io.EOF {
		l.Close()
	}
	return n, err
}

func (l *lazyFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}

// formPart is one part of a multipart body: a text field, or a file.
type formPart struct {
	field string
	value string
	file  *Input
}

func textPart(field, value string) formPart { return formPart{field: field, value: value} }

// recipePart is a recipe, which travels as a file named recipe.toml.
func recipePart(recipe string) formPart {
	return filePart("recipe", Bytes("recipe.toml", []byte(recipe)))
}
func filePart(field string, in Input) formPart {
	return formPart{field: field, file: &in}
}

// multipartBody streams parts as multipart/form-data: the part headers are
// rendered up front, the files are read as the body is sent.
func multipartBody(parts []formPart) (*requestBody, error) {
	replayable := true
	for _, p := range parts {
		if p.file == nil {
			continue
		}
		if err := p.file.validate(); err != nil {
			return nil, err
		}
		if !p.file.replayable() {
			replayable = false
		}
	}
	// One boundary for every attempt, so the header stays true.
	boundary := multipart.NewWriter(io.Discard).Boundary()

	open := func() (io.ReadCloser, int64, error) {
		var (
			buf     bytes.Buffer
			readers []io.Reader
			closers []io.Closer
			total   int64
		)
		fail := func(err error) (io.ReadCloser, int64, error) {
			for _, c := range closers {
				c.Close()
			}
			return nil, 0, err
		}
		flush := func() {
			if buf.Len() == 0 {
				return
			}
			chunk := bytes.Clone(buf.Bytes())
			buf.Reset()
			readers = append(readers, bytes.NewReader(chunk))
			if total >= 0 {
				total += int64(len(chunk))
			}
		}
		w := multipart.NewWriter(&buf)
		if err := w.SetBoundary(boundary); err != nil {
			return fail(err)
		}
		for _, p := range parts {
			if p.file == nil {
				if err := w.WriteField(p.field, p.value); err != nil {
					return fail(err)
				}
				continue
			}
			if _, err := w.CreateFormFile(p.field, p.file.fileName()); err != nil {
				return fail(err)
			}
			flush()
			r, size, err := p.file.open()
			if err != nil {
				return fail(err)
			}
			readers = append(readers, r)
			closers = append(closers, r)
			if size < 0 {
				total = -1
			} else if total >= 0 {
				total += size
			}
		}
		if err := w.Close(); err != nil {
			return fail(err)
		}
		flush()
		return &multiBody{Reader: io.MultiReader(readers...), closers: closers}, total, nil
	}

	return &requestBody{
		contentType: "multipart/form-data; boundary=" + boundary,
		open:        open,
		replayable:  replayable,
	}, nil
}

type multiBody struct {
	io.Reader
	closers []io.Closer
}

func (m *multiBody) Close() error {
	var first error
	for _, c := range m.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
