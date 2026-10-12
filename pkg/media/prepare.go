package media

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
)

// prepareImage reads an upload, checks it is an image this package accepts,
// and returns it ready to store: resized to MaxEdgePixels and re-encoded
// (see Normalise). Every image store uses it, so the rules are the same
// whether the bytes end up in Postgres or in R2.
//
// The type is decided from the file's first bytes, never from the request
// header: a caller can claim anything, and whatever is stored is later served
// to customers. One byte over the cap is a rejection rather than a
// truncation, because a cropped file that looks like a success is worse.
func prepareImage(r io.Reader) ([]byte, string, error) {
	var buf bytes.Buffer
	written, err := io.Copy(&buf, io.LimitReader(r, MaxUploadBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("media: read upload: %w", err)
	}
	if written > MaxUploadBytes {
		return nil, "", ErrTooLarge
	}
	if written == 0 {
		return nil, "", fmt.Errorf("%w: empty upload", ErrUnsupportedType)
	}

	detected := http.DetectContentType(buf.Bytes())
	if base, _, err := mime.ParseMediaType(detected); err == nil {
		detected = base
	}
	if _, ok := allowedContentTypes[detected]; !ok {
		return nil, "", fmt.Errorf("%w: %s", ErrUnsupportedType, detected)
	}
	return Normalise(buf.Bytes(), detected)
}

// prepareVideo reads an upload and checks it is an MP4, MOV or WebM within
// MaxVideoBytes. Videos are stored as uploaded.
func prepareVideo(r io.Reader) ([]byte, string, error) {
	var buf bytes.Buffer
	written, err := io.Copy(&buf, io.LimitReader(r, MaxVideoBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("media: read video upload: %w", err)
	}
	if written > MaxVideoBytes {
		return nil, "", ErrVideoTooLarge
	}
	contentType := detectVideoType(buf.Bytes())
	if contentType == "" {
		return nil, "", ErrUnsupportedVideo
	}
	return buf.Bytes(), contentType, nil
}
