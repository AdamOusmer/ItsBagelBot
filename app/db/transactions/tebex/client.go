// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tebex

import (
	"ItsBagelBot/pkg/codec"
	"bytes"
	"context"
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
	WebstoreToken   string
	PrivateKey      string
	IncludeUsername bool
	PackageID       int
	PackageType     string
	CompleteURL     string
	CancelURL       string
	BaseURL         string
	HTTPClient      *http.Client
}

type Client struct {
	cfg Config
}

type Basket struct {
	Ident       string
	CheckoutURL string
}

type basketData struct {
	Data struct {
		Ident string      `json:"ident"`
		Links basketLinks `json:"links"`
	} `json:"data"`
}

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
		if err := codec.Unmarshal(data, &obj); err != nil {
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
		if err := codec.Unmarshal(data, &arr); err != nil {
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
		if err := codec.Unmarshal(data, &checkout); err != nil {
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

type BasketSpec struct {
	UserID        uint64
	Username      string
	IPAddress     string
	GiftedByID    uint64
	GiftedByLogin string
	PackageType   string
	GiftMessage   string
}

// Tebex echoes the custom payload on the payment webhook: it is the whole attribution chain.
func (c *Client) CreateBasket(ctx context.Context, spec BasketSpec) (Basket, error) {

	custom := map[string]string{
		"user_id":  strconv.FormatUint(spec.UserID, 10),
		"username": spec.Username,
	}
	if spec.GiftedByID != 0 {
		custom["gifted_by"] = strconv.FormatUint(spec.GiftedByID, 10)
		custom["gifted_by_login"] = spec.GiftedByLogin
		if spec.GiftMessage != "" {
			custom["gift_message"] = spec.GiftMessage
		}
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

	packageType := c.cfg.PackageType
	if spec.PackageType != "" {
		packageType = spec.PackageType
	}

	addPackage := map[string]any{
		"package_id": c.cfg.PackageID,
		"quantity":   1,
		"type":       packageType,
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

	body, err := codec.Marshal(payload)
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

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tebex responded %d: %s", resp.StatusCode, truncate(data, 300))
	}

	return codec.Unmarshal(data, out)
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}
