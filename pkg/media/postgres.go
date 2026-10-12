package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStorer keeps uploaded images in the application's own database.
//
// It exists because the obvious alternative does not survive the way this
// application is deployed: LocalStorer writes to the container filesystem, and
// on a platform that replaces the container on every deploy that means the
// catalogue's photography disappears each time the service ships. Images in
// Postgres are backed up, replicated and restored with everything else, and
// need no third-party account.
//
// The trade-offs are real and worth stating:
//
//   - No resizing. Like LocalStorer, every variant resolves to the original,
//     so a grid thumbnail downloads the full image. CloudflareImages is the
//     implementation to reach for when that starts to matter.
//   - Row size. Each image is capped at MaxUploadBytes (10 MiB) and stored
//     inline, so Postgres TOASTs it. Fine for a catalogue of hundreds; not
//     where tens of thousands of images belong.
//   - The bytes pass through this server on the way in and on the way out,
//     which is what the Storer interface's ticket flow otherwise avoids.
type PostgresStorer struct {
	pool *pgxpool.Pool
	// publicBaseURL is the origin a browser reaches this API on. Absolute,
	// because the storefront runs somewhere else entirely.
	publicBaseURL string
}

func NewPostgresStorer(pool *pgxpool.Pool, publicBaseURL string) (*PostgresStorer, error) {
	if pool == nil {
		return nil, errors.New("media: postgres storer needs a pool")
	}
	if publicBaseURL == "" {
		return nil, errors.New("media: PUBLIC_BASE_URL must be set for image delivery URLs")
	}
	return &PostgresStorer{pool: pool, publicBaseURL: strings.TrimRight(publicBaseURL, "/")}, nil
}

func (p *PostgresStorer) Enabled() bool { return p.pool != nil }

// CreateUploadTicket reserves a random id and points the upload back here.
//
// Nothing is written until the bytes arrive, so an abandoned ticket costs a
// row of nothing. The id is random rather than sequential so delivery URLs
// cannot be walked to enumerate the catalogue's images.
func (p *PostgresStorer) CreateUploadTicket(context.Context) (*UploadTicket, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("media: generate image id: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	return &UploadTicket{
		ID:          id,
		UploadURL:   fmt.Sprintf("%s/v1/admin/images/%s/content", p.publicBaseURL, id),
		DeliveryURL: p.DeliveryURL(id, VariantPublic),
	}, nil
}

// DeliveryURL ignores the variant — there is one rendition per id.
func (p *PostgresStorer) DeliveryURL(imageID, _ string) string {
	if imageID == "" {
		return ""
	}
	return fmt.Sprintf("%s/v1/media/%s", p.publicBaseURL, imageID)
}

// Receive reads the upload, checks it, and stores it.
//
// The content type is decided from the first bytes rather than the request
// header: a caller can claim anything, and whatever lands here is later served
// back from our own origin. The whole image is held in memory once — bounded
// by MaxUploadBytes, which is the same ceiling the disk storer enforces.
func (p *PostgresStorer) Receive(ctx context.Context, imageID, _ string, r io.Reader) error {
	if err := validID(imageID); err != nil {
		return err
	}

	stored, storedType, err := prepareImage(r)
	if err != nil {
		return err
	}

	// Upsert so a retried upload against the same ticket replaces rather than
	// conflicts — the browser retrying a flaky upload is the common case.
	const q = `
		INSERT INTO media_objects (id, content_type, byte_size, bytes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		   SET content_type = EXCLUDED.content_type,
		       byte_size    = EXCLUDED.byte_size,
		       bytes        = EXCLUDED.bytes`
	if _, err := p.pool.Exec(ctx, q, imageID, storedType, len(stored), stored); err != nil {
		return fmt.Errorf("media: store %s: %w", imageID, err)
	}
	return nil
}

// Open returns the stored image.
//
// The signature is the file-shaped one the Receiver interface asks for, so the
// bytes are wrapped in a reader the handler closes like any other.
func (p *PostgresStorer) Open(imageID string) (io.ReadCloser, string, error) {
	if err := validID(imageID); err != nil {
		return nil, "", err
	}
	var contentType string
	var data []byte
	err := p.pool.QueryRow(context.Background(),
		`SELECT content_type, bytes FROM media_objects WHERE id = $1`, imageID).
		Scan(&contentType, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("media: read %s: %w", imageID, err)
	}
	return io.NopCloser(bytes.NewReader(data)), contentType, nil
}

// Delete removes the image. Already gone counts as success.
func (p *PostgresStorer) Delete(ctx context.Context, imageID string) error {
	if err := validID(imageID); err != nil {
		return err
	}
	if _, err := p.pool.Exec(ctx, `DELETE FROM media_objects WHERE id = $1`, imageID); err != nil {
		return fmt.Errorf("media: delete %s: %w", imageID, err)
	}
	return nil
}
