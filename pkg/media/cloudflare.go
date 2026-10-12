package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotConfigured reports that no image provider is set up.
var ErrNotConfigured = errors.New("image storage is not configured")

const (
	cloudflareAPIBase = "https://api.cloudflare.com/client/v4"
	deliveryBase      = "https://imagedelivery.net"
)

// CloudflareImages stores images in Cloudflare Images.
//
// Chosen over R2 (or any plain object store) because resizing comes with it:
// one upload serves a 200px grid thumbnail and a 1600px product hero through
// named variants, so the admin uploads a single file and the storefront asks
// for the size it needs. With a bucket, every rendition would have to be
// generated and stored by us.
type CloudflareImages struct {
	accountID  string
	apiToken   string
	accountsub string // the delivery-URL account hash, not the account id
	httpClient *http.Client
	// apiBase is Cloudflare's API root, overridable so tests can point at a
	// stub and pin the request shape without network access. Never set in
	// production code.
	apiBase string
}

// NewCloudflareImages builds a Storer.
//
// accountHash is the string in delivery URLs
// (https://imagedelivery.net/<hash>/<id>/<variant>). It is deliberately
// distinct from accountID: the two look alike, are easy to confuse, and
// swapping them yields URLs that 404 with no other symptom.
func NewCloudflareImages(accountID, apiToken, accountHash string) *CloudflareImages {
	return &CloudflareImages{
		accountID:  accountID,
		apiToken:   apiToken,
		accountsub: accountHash,
		// Generous but bounded: this call only brokers a ticket, it does not
		// carry the image, so it should be quick.
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiBase:    cloudflareAPIBase,
	}
}

func (c *CloudflareImages) Enabled() bool {
	return c.accountID != "" && c.apiToken != "" && c.accountsub != ""
}

// cloudflareEnvelope is the shape every Cloudflare v4 endpoint answers with.
type cloudflareEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result struct {
		ID        string `json:"id"`
		UploadURL string `json:"uploadURL"`
	} `json:"result"`
}

// err turns Cloudflare's error array into one message worth logging.
func (e cloudflareEnvelope) err() error {
	if len(e.Errors) == 0 {
		return errors.New("cloudflare rejected the request without saying why")
	}
	parts := make([]string, 0, len(e.Errors))
	for _, x := range e.Errors {
		parts = append(parts, fmt.Sprintf("%d: %s", x.Code, x.Message))
	}
	return errors.New(strings.Join(parts, "; "))
}

// CreateUploadTicket asks Cloudflare for a one-time direct-upload URL.
//
// `requireSignedURLs=false` because these are product photographs on a public
// storefront — signing them would add a token to every <img> for no benefit,
// and the URLs are already unguessable.
func (c *CloudflareImages) CreateUploadTicket(ctx context.Context) (*UploadTicket, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}

	form := url.Values{}
	form.Set("requireSignedURLs", "false")

	endpoint := fmt.Sprintf("%s/accounts/%s/images/v2/direct_upload", c.apiBase, c.accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build direct-upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare images request failed: %w", err)
	}
	defer resp.Body.Close()

	var env cloudflareEnvelope
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("cloudflare responded %d with unreadable body: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	if !env.Success || env.Result.UploadURL == "" {
		return nil, fmt.Errorf("cloudflare images: %w", env.err())
	}

	return &UploadTicket{
		ID:          env.Result.ID,
		UploadURL:   env.Result.UploadURL,
		DeliveryURL: c.DeliveryURL(env.Result.ID, VariantPublic),
	}, nil
}

// Delete removes an image permanently.
//
// A 404 counts as success: the caller wanted the image gone, and it is. Any
// other failure is real and returned, so a delete that silently left the file
// costing money does not look like a delete that worked.
func (c *CloudflareImages) Delete(ctx context.Context, imageID string) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}

	endpoint := fmt.Sprintf("%s/accounts/%s/images/v1/%s", c.apiBase, c.accountID, url.PathEscape(imageID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare images delete failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("cloudflare images delete responded %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}
	return nil
}

// DeliveryURL builds the public URL. No network call.
func (c *CloudflareImages) DeliveryURL(imageID, variant string) string {
	if imageID == "" || !c.Enabled() {
		return ""
	}
	if variant == "" {
		variant = VariantPublic
	}
	return fmt.Sprintf("%s/%s/%s/%s", deliveryBase, c.accountsub, imageID, variant)
}
