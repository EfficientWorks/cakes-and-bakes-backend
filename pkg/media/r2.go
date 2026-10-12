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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

/*
 * Cloudflare R2: images and videos as objects, served from R2's public URL.
 *
 * Uploads still come to this server first (the admin posts to the ticket's
 * uploadUrl, exactly as with the database store). That is where each file is
 * checked and an image is resized, so nothing unvetted ever reaches the
 * bucket. From then on the API is out of the way: pages load straight from
 * R2's public host, through Cloudflare's CDN, with no egress charge.
 *
 * Objects are keyed images/<id> and videos/<id>, with their content type and
 * a one-year immutable Cache-Control stored on the object. An id is random
 * and never reused, so the bytes behind a URL never change.
 */

// R2Config is the bucket and how to reach it. All fields are required.
type R2Config struct {
	AccountID string
	AccessKey string
	SecretKey string
	Bucket    string
	// PublicHost is the bucket's public origin: its custom domain
	// (https://media.example.com) or its r2.dev address.
	PublicHost string
	// APIBaseURL is this API's public origin; upload tickets point here.
	APIBaseURL string
}

// Complete reports whether every value is present. Two out of six is a
// misconfiguration, not an R2 setup.
func (c R2Config) Complete() bool {
	return c.AccountID != "" && c.AccessKey != "" && c.SecretKey != "" &&
		c.Bucket != "" && c.PublicHost != "" && c.APIBaseURL != ""
}

// objectStore is the slice of S3 this package uses; a fake replaces it in tests.
type objectStore interface {
	put(ctx context.Context, key, contentType string, body []byte) error
	get(ctx context.Context, key string) (io.ReadCloser, string, error)
	remove(ctx context.Context, key string) error
}

// r2Bucket talks to R2 through its S3-compatible API.
type r2Bucket struct {
	client *s3.Client
	bucket string
}

func newR2Bucket(cfg R2Config) *r2Bucket {
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: true,
		// Newer SDK versions add CRC checksums to every request by default;
		// Cloudflare's R2 guidance is to send them only when required.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &r2Bucket{client: client, bucket: cfg.Bucket}
}

const immutableCache = "public, max-age=31536000, immutable"

func (b *r2Bucket) put(ctx context.Context, key, contentType string, body []byte) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType),
		CacheControl:  aws.String(immutableCache),
	})
	if err != nil {
		return fmt.Errorf("media: r2 put %s: %w", key, err)
	}
	return nil
}

func (b *r2Bucket) get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(b.bucket), Key: aws.String(key)})
	var missing *s3types.NoSuchKey
	if errors.As(err, &missing) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("media: r2 get %s: %w", key, err)
	}
	return out.Body, aws.ToString(out.ContentType), nil
}

// remove deletes an object. S3 deletes are idempotent: an already-missing
// key is not an error.
func (b *r2Bucket) remove(ctx context.Context, key string) error {
	if _, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b.bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("media: r2 delete %s: %w", key, err)
	}
	return nil
}

// PublicDelivery is a store whose files are served from their own public
// host. The API's /v1/media routes redirect there instead of streaming bytes,
// so links stored before a move to R2 keep working.
type PublicDelivery interface {
	PublicURL(id string) string
}

// R2ImageStore keeps images in R2. It implements Storer and Receiver.
type R2ImageStore struct {
	objects    objectStore
	publicHost string
	apiBaseURL string
}

// NewR2ImageStore returns an image store on the configured bucket.
func NewR2ImageStore(cfg R2Config) *R2ImageStore {
	return &R2ImageStore{objects: newR2Bucket(cfg), publicHost: trimSlash(cfg.PublicHost), apiBaseURL: trimSlash(cfg.APIBaseURL)}
}

func (s *R2ImageStore) Enabled() bool { return s.objects != nil }

// CreateUploadTicket reserves a random id; the admin posts the file to this
// API, which checks and resizes it before it goes to R2.
func (s *R2ImageStore) CreateUploadTicket(context.Context) (*UploadTicket, error) {
	id, err := newMediaID()
	if err != nil {
		return nil, err
	}
	return &UploadTicket{
		ID:          id,
		UploadURL:   fmt.Sprintf("%s/v1/admin/images/%s/content", s.apiBaseURL, id),
		DeliveryURL: s.PublicURL(id),
	}, nil
}

// Receive checks, resizes and stores an uploaded image.
func (s *R2ImageStore) Receive(ctx context.Context, imageID, _ string, r io.Reader) error {
	if err := validID(imageID); err != nil {
		return err
	}
	data, contentType, err := prepareImage(r)
	if err != nil {
		return err
	}
	return s.objects.put(ctx, imageKey(imageID), contentType, data)
}

// Open reads an image back. Delivery normally goes straight to PublicURL;
// this exists so the Receiver contract holds.
func (s *R2ImageStore) Open(imageID string) (io.ReadCloser, string, error) {
	if err := validID(imageID); err != nil {
		return nil, "", err
	}
	return s.objects.get(context.Background(), imageKey(imageID))
}

func (s *R2ImageStore) Delete(ctx context.Context, imageID string) error {
	if err := validID(imageID); err != nil {
		return err
	}
	return s.objects.remove(ctx, imageKey(imageID))
}

// DeliveryURL ignores the variant: one stored rendition per id.
func (s *R2ImageStore) DeliveryURL(imageID, _ string) string {
	if imageID == "" {
		return ""
	}
	return s.PublicURL(imageID)
}

func (s *R2ImageStore) PublicURL(id string) string { return s.publicHost + "/" + imageKey(id) }

// R2VideoStore keeps product videos in R2. R2 answers Range requests itself,
// which is what video playback and seeking need.
type R2VideoStore struct {
	objects    objectStore
	publicHost string
	apiBaseURL string
}

// NewR2VideoStore returns a video store on the configured bucket.
func NewR2VideoStore(cfg R2Config) *R2VideoStore {
	return &R2VideoStore{objects: newR2Bucket(cfg), publicHost: trimSlash(cfg.PublicHost), apiBaseURL: trimSlash(cfg.APIBaseURL)}
}

func (s *R2VideoStore) CreateUploadTicket(context.Context) (*UploadTicket, error) {
	id, err := newMediaID()
	if err != nil {
		return nil, err
	}
	return &UploadTicket{
		ID:          id,
		UploadURL:   fmt.Sprintf("%s/v1/admin/videos/%s/content", s.apiBaseURL, id),
		DeliveryURL: s.PublicURL(id),
	}, nil
}

// Receive checks and stores an uploaded video.
func (s *R2VideoStore) Receive(ctx context.Context, id string, r io.Reader) error {
	if err := validID(id); err != nil {
		return err
	}
	data, contentType, err := prepareVideo(r)
	if err != nil {
		return err
	}
	return s.objects.put(ctx, videoKey(id), contentType, data)
}

func (s *R2VideoStore) Delete(ctx context.Context, id string) error {
	if err := validID(id); err != nil {
		return err
	}
	return s.objects.remove(ctx, videoKey(id))
}

func (s *R2VideoStore) DeliveryURL(id string) string { return s.PublicURL(id) }

func (s *R2VideoStore) PublicURL(id string) string { return s.publicHost + "/" + videoKey(id) }

func imageKey(id string) string { return "images/" + id }
func videoKey(id string) string { return "videos/" + id }

// newMediaID is random rather than sequential, so delivery URLs cannot be
// walked to enumerate a catalogue's files.
func newMediaID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("media: generate id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func trimSlash(v string) string { return strings.TrimRight(v, "/") }
