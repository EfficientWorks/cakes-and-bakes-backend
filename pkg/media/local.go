package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Receiver is implemented by storers that take delivery of the bytes
// themselves, rather than handing the browser a provider URL to upload to.
//
// Cloudflare does not implement it: there the browser posts straight to
// Cloudflare and this server never sees the file. Local storage does, because
// there is nowhere else for the file to go. Handlers type-assert for it, so
// the two behave identically from the admin UI's point of view — same ticket,
// same two-step upload — and only the host in the upload URL differs.
type Receiver interface {
	// Receive stores the bytes for an id previously handed out in a ticket.
	Receive(ctx context.Context, imageID, contentType string, r io.Reader) error
	// Open returns the stored bytes and their content type for serving.
	Open(imageID string) (io.ReadCloser, string, error)
}

// MaxUploadBytes caps a single image.
//
// Product photography from a phone is a few megabytes; ten leaves generous
// headroom while stopping a stray video from filling the disk. Enforced when
// reading, not from the Content-Length header, which a client controls.
const MaxUploadBytes = 10 << 20 // 10 MiB

// allowedContentTypes is what may be stored, keyed to the extension used on
// disk.
//
// An allowlist rather than a blocklist, and checked against the bytes rather
// than the filename: the whole risk here is storing something that is not an
// image and later serving it back from our own origin.
var allowedContentTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/avif": ".avif",
	"image/gif":  ".gif",
}

// LocalStorer keeps images on the server's own disk.
//
// For development and small single-server deployments. It has two limits worth
// knowing before it is used in anger:
//
//   - No resizing. Every variant resolves to the same original file, so a grid
//     thumbnail downloads the full-size image. Cloudflare Images exists to fix
//     exactly that; swap the implementation when it matters.
//   - Local disk. Two application instances do not share it, and a container
//     without a mounted volume loses everything on restart.
type LocalStorer struct {
	dir string
	// publicBaseURL is the origin a browser can reach this server on. It has
	// to be absolute and it has to be configured: the storefront runs on a
	// different origin from the API, so a relative URL would resolve against
	// the wrong host.
	publicBaseURL string
}

// NewLocalStorer prepares the storage directory.
//
// Failure to create it is returned rather than swallowed — an image store that
// cannot write is worth refusing to start over.
func NewLocalStorer(dir, publicBaseURL string) (*LocalStorer, error) {
	if dir == "" {
		return nil, errors.New("media: local storage directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("media: create %s: %w", dir, err)
	}
	return &LocalStorer{
		dir:           dir,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}, nil
}

func (l *LocalStorer) Enabled() bool { return l.dir != "" }

// CreateUploadTicket reserves an id and points the upload at this server.
//
// The id is random rather than sequential so a delivery URL cannot be walked
// to enumerate every image, and so two uploads can never collide.
func (l *LocalStorer) CreateUploadTicket(context.Context) (*UploadTicket, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("media: generate image id: %w", err)
	}
	id := hex.EncodeToString(raw[:])

	return &UploadTicket{
		ID:          id,
		UploadURL:   fmt.Sprintf("%s/v1/admin/images/%s/content", l.publicBaseURL, id),
		DeliveryURL: l.DeliveryURL(id, VariantPublic),
	}, nil
}

// DeliveryURL ignores the variant: there is only ever one rendition on disk.
//
// The parameter is kept so callers do not have to know which storer they have,
// and so switching to Cloudflare later is a configuration change rather than a
// code change at every call site.
func (l *LocalStorer) DeliveryURL(imageID, _ string) string {
	if imageID == "" {
		return ""
	}
	return fmt.Sprintf("%s/v1/media/%s", l.publicBaseURL, imageID)
}

// Receive writes the upload to disk.
//
// The content type is taken from the bytes, not from the request: a caller can
// claim anything in a Content-Type header, and this file is served back from
// our own origin later.
func (l *LocalStorer) Receive(_ context.Context, imageID, _ string, r io.Reader) error {
	if err := validID(imageID); err != nil {
		return err
	}

	// Enough for http.DetectContentType, which only reads the first 512.
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("media: read upload: %w", err)
	}
	head = head[:n]

	detected := http.DetectContentType(head)
	// DetectContentType can return "image/jpeg; charset=..." shapes.
	if base, _, err := mime.ParseMediaType(detected); err == nil {
		detected = base
	}
	if _, ok := allowedContentTypes[detected]; !ok {
		return fmt.Errorf("%w: %s", ErrUnsupportedType, detected)
	}

	// The whole file has to be in hand to resize it, so unlike the streaming
	// path this reads it all first — bounded by MaxUploadBytes either way.
	body := io.MultiReader(bytes.NewReader(head), r)
	var raw bytes.Buffer
	written, err := io.Copy(&raw, io.LimitReader(body, MaxUploadBytes+1))
	if err != nil {
		return fmt.Errorf("media: read upload: %w", err)
	}
	// One byte over the cap is a rejection, not a truncation: silently storing
	// a cropped file would be worse than refusing it.
	if written > MaxUploadBytes {
		return ErrTooLarge
	}

	stored, storedType, err := Normalise(raw.Bytes(), detected)
	if err != nil {
		return err
	}
	ext := allowedContentTypes[storedType]

	path := filepath.Join(l.dir, imageID+ext)
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("media: create %s: %w", path, err)
	}

	// Closed explicitly rather than by defer, because the failure path below
	// deletes this file — and on Windows an open file cannot be removed, so a
	// deferred close would leave every rejected upload on disk.
	if _, err := file.Write(stored); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("media: write %s: %w", path, err)
	}

	if err := file.Close(); err != nil {
		// A close error means the bytes may not have reached the disk, so the
		// partial file must not be left looking like a successful upload.
		_ = os.Remove(path)
		return fmt.Errorf("media: finalise %s: %w", path, err)
	}
	return nil
}

// Open returns the stored file and its content type.
func (l *LocalStorer) Open(imageID string) (io.ReadCloser, string, error) {
	if err := validID(imageID); err != nil {
		return nil, "", err
	}

	for contentType, ext := range allowedContentTypes {
		file, err := os.Open(filepath.Join(l.dir, imageID+ext))
		if err == nil {
			return file, contentType, nil
		}
		if !os.IsNotExist(err) {
			return nil, "", fmt.Errorf("media: open %s: %w", imageID, err)
		}
	}
	return nil, "", ErrNotFound
}

// Delete removes the file. Already gone counts as success.
func (l *LocalStorer) Delete(_ context.Context, imageID string) error {
	if err := validID(imageID); err != nil {
		return err
	}
	for _, ext := range allowedContentTypes {
		if err := os.Remove(filepath.Join(l.dir, imageID+ext)); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("media: delete %s: %w", imageID, err)
		}
	}
	return nil
}

// validID rejects anything that is not one of our generated ids.
//
// Ids reach this package from a URL path, and they are used to build a
// filename. Restricting them to lowercase hex means no separator, no dot and
// no traversal sequence can survive — `..` and `/` simply are not valid
// characters, so there is nothing to escape or normalise.
func validID(id string) error {
	if len(id) != 32 {
		return ErrInvalidID
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ErrInvalidID
		}
	}
	return nil
}

var (
	// ErrUnsupportedType reports a file that is not an image we will store.
	ErrUnsupportedType = errors.New("media: unsupported image type")
	// ErrTooLarge reports an upload beyond MaxUploadBytes.
	ErrTooLarge = errors.New("media: image is too large")
	// ErrNotFound reports an image id with nothing stored against it.
	ErrNotFound = errors.New("media: image not found")
	// ErrInvalidID reports an id that this package did not issue.
	ErrInvalidID = errors.New("media: invalid image id")
)
