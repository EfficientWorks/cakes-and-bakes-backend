package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

/*
 * Product videos.
 *
 * One short video per product, shown with its photographs on the website.
 * Kept in Postgres (media_videos) for the same reason images are: the
 * container filesystem does not survive a deploy, and no third-party account
 * is needed. Independent of the image provider, so it works whether images go
 * to Cloudflare or to the database.
 *
 * Browsers play video by asking for byte ranges (Safari insists on it), so
 * reads never load the whole file: VideoReader fetches only the chunk asked
 * for, and the column is stored EXTERNAL so Postgres can slice it without
 * decompressing everything first.
 */

// MaxVideoBytes caps one video. Product clips are short; this keeps an upload
// (which is held in memory once on the way in) bounded.
const MaxVideoBytes = 50 << 20 // 50 MiB

var (
	// ErrUnsupportedVideo is a file that is not MP4, MOV or WebM.
	ErrUnsupportedVideo = errors.New("media: unsupported video type")
	// ErrVideoTooLarge is a video over MaxVideoBytes.
	ErrVideoTooLarge = errors.New("media: video is too large")
)

// VideoStore keeps product videos in the database.
type VideoStore struct {
	pool          *pgxpool.Pool
	publicBaseURL string
}

// NewVideoStore needs the pool and the API's public origin (for delivery URLs).
func NewVideoStore(pool *pgxpool.Pool, publicBaseURL string) (*VideoStore, error) {
	if pool == nil {
		return nil, errors.New("media: video store needs a pool")
	}
	if publicBaseURL == "" {
		return nil, errors.New("media: PUBLIC_BASE_URL must be set for video delivery URLs")
	}
	return &VideoStore{pool: pool, publicBaseURL: strings.TrimRight(publicBaseURL, "/")}, nil
}

// CreateUploadTicket reserves a random id; nothing is stored until the bytes
// arrive at UploadURL.
func (v *VideoStore) CreateUploadTicket(context.Context) (*UploadTicket, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("media: generate video id: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	return &UploadTicket{
		ID:          id,
		UploadURL:   fmt.Sprintf("%s/v1/admin/videos/%s/content", v.publicBaseURL, id),
		DeliveryURL: v.DeliveryURL(id),
	}, nil
}

// DeliveryURL is where a stored video is served from (public).
func (v *VideoStore) DeliveryURL(id string) string {
	return fmt.Sprintf("%s/v1/media/videos/%s", v.publicBaseURL, id)
}

// Receive checks and stores an uploaded video. The type is decided from the
// file's own first bytes, never from what the client claims.
func (v *VideoStore) Receive(ctx context.Context, id string, r io.Reader) error {
	if err := validID(id); err != nil {
		return err
	}
	data, contentType, err := prepareVideo(r)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO media_videos (id, content_type, byte_size, bytes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		   SET content_type = EXCLUDED.content_type,
		       byte_size    = EXCLUDED.byte_size,
		       bytes        = EXCLUDED.bytes,
		       created_at   = NOW()`
	if _, err := v.pool.Exec(ctx, q, id, contentType, len(data), data); err != nil {
		return fmt.Errorf("media: store video %s: %w", id, err)
	}
	return nil
}

// detectVideoType recognises MP4/MOV (an ISO base media "ftyp" box) and WebM
// (an EBML header). Anything else is refused.
func detectVideoType(b []byte) string {
	if len(b) >= 12 && string(b[4:8]) == "ftyp" {
		if string(b[8:10]) == "qt" {
			return "video/quicktime"
		}
		return "video/mp4"
	}
	if len(b) >= 4 && b[0] == 0x1A && b[1] == 0x45 && b[2] == 0xDF && b[3] == 0xA3 {
		return "video/webm"
	}
	return ""
}

// VideoInfo describes a stored video without reading it.
type VideoInfo struct {
	ContentType string
	Size        int64
	ModTime     time.Time
}

// Stat returns a video's type, size and upload time.
func (v *VideoStore) Stat(ctx context.Context, id string) (*VideoInfo, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	var info VideoInfo
	err := v.pool.QueryRow(ctx,
		`SELECT content_type, byte_size, created_at FROM media_videos WHERE id = $1`, id).
		Scan(&info.ContentType, &info.Size, &info.ModTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("media: stat video %s: %w", id, err)
	}
	return &info, nil
}

// Delete removes a video. Already gone counts as success.
func (v *VideoStore) Delete(ctx context.Context, id string) error {
	if err := validID(id); err != nil {
		return err
	}
	if _, err := v.pool.Exec(ctx, `DELETE FROM media_videos WHERE id = $1`, id); err != nil {
		return fmt.Errorf("media: delete video %s: %w", id, err)
	}
	return nil
}

// videoChunk is how much one database read fetches.
const videoChunk = 1 << 20 // 1 MiB

// NewReader returns a ReadSeeker over a stored video that reads it from the
// database a chunk at a time — what http.ServeContent needs to answer Range
// requests without holding the whole file in memory.
func (v *VideoStore) NewReader(ctx context.Context, id string, size int64) *VideoReader {
	return &VideoReader{ctx: ctx, pool: v.pool, id: id, size: size, bufStart: -1}
}

// VideoReader is an io.ReadSeeker over one media_videos row.
type VideoReader struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	id       string
	size     int64
	pos      int64
	buf      []byte
	bufStart int64
}

func (r *VideoReader) Read(p []byte) (int, error) {
	if r.pos >= r.size {
		return 0, io.EOF
	}
	if r.bufStart < 0 || r.pos < r.bufStart || r.pos >= r.bufStart+int64(len(r.buf)) {
		n := int64(videoChunk)
		if r.pos+n > r.size {
			n = r.size - r.pos
		}
		var chunk []byte
		// substring is 1-based.
		if err := r.pool.QueryRow(r.ctx,
			`SELECT substring(bytes from $2 for $3) FROM media_videos WHERE id = $1`,
			r.id, r.pos+1, n).Scan(&chunk); err != nil {
			return 0, fmt.Errorf("media: read video %s: %w", r.id, err)
		}
		if len(chunk) == 0 {
			return 0, io.EOF
		}
		r.buf, r.bufStart = chunk, r.pos
	}
	n := copy(p, r.buf[r.pos-r.bufStart:])
	r.pos += int64(n)
	return n, nil
}

func (r *VideoReader) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.pos + offset
	case io.SeekEnd:
		next = r.size + offset
	default:
		return 0, errors.New("media: invalid whence")
	}
	if next < 0 {
		return 0, errors.New("media: negative position")
	}
	r.pos = next
	return next, nil
}
