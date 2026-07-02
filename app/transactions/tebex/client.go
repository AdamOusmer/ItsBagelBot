// Package tebex is the minimal Tebex Headless API client the checkout RPC
// needs: create a basket carrying the buyer's user id and put the premium
// package in it. Everything else (payment, receipts, subscription state)
// stays on Tebex's side and comes back through the webhook.
package tebex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://headless.tebex.io"

type Config struct {
	// WebstoreToken is the public Headless API token (Tebex webstore identifier).
	WebstoreToken string
	// PrivateKey is the Headless API private key. When set, backend-created
	// baskets use HTTP Basic auth (public token as username, private key as
	// password) so Tebex accepts the customer's IPv4 address.
	PrivateKey string
	// IncludeUsername sends the store/customer username at Tebex's top level.
	// Minecraft/Overwolf stores require it; Universal stores reject it. Twitch
	// account attribution for ItsBagelBot does not depend on this field because
	// user_id/username are always carried in the custom payload.
	IncludeUsername bool
	// PackageID is the premium package to place in every basket.
	PackageID int
	// PackageType is "subscription" or "single"; premium is a monthly
	// subscription so that is the default.
	PackageType string
	// CompleteURL / CancelURL are where hosted checkout returns the browser.
	CompleteURL string
	CancelURL   string
	BaseURL     string
	HTTPClient  *http.Client
}

type Client struct {
	cfg Config
}

type Basket struct {
	Ident       string
	CheckoutURL string
}

// basketData is the shared shape of the Headless API's basket envelope.
type basketData struct {
	Data struct {
		Ident string      `json:"ident"`
		Links basketLinks `json:"links"`
	} `json:"data"`
}

// basketLinks accepts both Tebex shapes seen in production/docs:
//   - {"checkout":"https://pay.tebex.io/..."} once a package is in the basket
//   - [] on a freshly created basket before packages have been added
//
// Some API surfaces also model links as rel/href arrays, so tolerate that too.
type basketLinks struct {
	Checkout string
}

func (l *basketLinks) UnmarshalJSON(data []byte) error {

	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	switch data[0] {
	case '{':
		var obj struct {
			Checkout string `json:"checkout"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		l.Checkout = obj.Checkout
		return nil
	case '[':
		var arr []struct {
			Rel      string `json:"rel"`
			Name     string `json:"name"`
			Href     string `json:"href"`
			URL      string `json:"url"`
			Checkout string `json:"checkout"`
		}
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		for _, link := range arr {
			if link.Checkout != "" {
				l.Checkout = link.Checkout
				return nil
			}
			if strings.EqualFold(link.Rel, "checkout") || strings.EqualFold(link.Name, "checkout") {
				if link.Href != "" {
					l.Checkout = link.Href
					return nil
				}
				if link.URL != "" {
					l.Checkout = link.URL
					return nil
				}
			}
		}
		return nil
	case '"':
		var checkout string
		if err := json.Unmarshal(data, &checkout); err != nil {
			return err
		}
		l.Checkout = checkout
		return nil
	default:
		return fmt.Errorf("unexpected basket links shape: %s", truncate(data, 80))
	}
}

func New(cfg Config) (*Client, error) {
	if cfg.WebstoreToken == "" {
		return nil, errors.New("tebex webstore token required")
	}
	if cfg.PackageID <= 0 {
		return nil, errors.New("tebex package id required")
	}
	if cfg.PackageType == "" {
		cfg.PackageType = "subscription"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{cfg: cfg}, nil
}

// BasketSpec names who the entitlement lands on (UserID/Username) and, for
// gifts, who pays (GiftedByID/GiftedByLogin). For a self-purchase the gifted-by
// fields stay zero.
type BasketSpec struct {
	UserID        uint64
	Username      string
	IPAddress     string
	GiftedByID    uint64
	GiftedByLogin string
}

// CreateBasket mints a basket and adds the premium package. The recipient's
// user id rides in the basket's custom payload, which Tebex echoes back on the
// payment webhook — that is the whole attribution chain; gifted_by is carried
// alongside for the gift notification and the audit trail.
func (c *Client) CreateBasket(ctx context.Context, spec BasketSpec) (Basket, error) {

	custom := map[string]string{
		"user_id":  strconv.FormatUint(spec.UserID, 10),
		"username": spec.Username,
	}
	if spec.GiftedByID != 0 {
		custom["gifted_by"] = strconv.FormatUint(spec.GiftedByID, 10)
		custom["gifted_by_login"] = spec.GiftedByLogin
	}

	create := map[string]any{
		"complete_url":           c.cfg.CompleteURL,
		"cancel_url":             c.cfg.CancelURL,
		"complete_auto_redirect": true,
		"custom":                 custom,
	}
	if c.cfg.IncludeUsername && spec.Username != "" {
		create["username"] = spec.Username
	}
	if spec.IPAddress != "" && c.cfg.PrivateKey != "" {
		create["ip_address"] = spec.IPAddress
	}

	var created basketData
	createPath := fmt.Sprintf("/api/accounts/%s/baskets", url.PathEscape(c.cfg.WebstoreToken))
	if err := c.post(ctx, createPath, create, &created); err != nil {
		return Basket{}, fmt.Errorf("create basket: %w", err)
	}
	if created.Data.Ident == "" {
		return Basket{}, errors.New("create basket: response missing ident")
	}

	addPackage := map[string]any{
		"package_id": c.cfg.PackageID,
		"quantity":   1,
		"type":       c.cfg.PackageType,
	}

	var updated basketData
	addPath := fmt.Sprintf("/api/baskets/%s/packages", url.PathEscape(created.Data.Ident))
	if err := c.post(ctx, addPath, addPackage, &updated); err != nil {
		return Basket{}, fmt.Errorf("add package: %w", err)
	}

	checkout := updated.Data.Links.Checkout
	if checkout == "" {
		checkout = created.Data.Links.Checkout
	}

	return Basket{Ident: created.Data.Ident, CheckoutURL: checkout}, nil
}

func (c *Client) post(ctx context.Context, path string, payload any, out any) error {

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.cfg.PrivateKey != "" && strings.HasPrefix(path, "/api/accounts/") {
		req.SetBasicAuth(c.cfg.WebstoreToken, c.cfg.PrivateKey)
	}

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Cap the read: Tebex baskets are small, and an error body only needs enough
	// bytes to be diagnosable.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tebex responded %d: %s", resp.StatusCode, truncate(data, 300))
	}

	return json.Unmarshal(data, out)
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}
