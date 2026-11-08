// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlay

import (
	"io/fs"
	"mime"
	"path"

	"github.com/gofiber/fiber/v3"
)

const contentPolicy = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

func boundary(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set("Referrer-Policy", "no-referrer")
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Content-Security-Policy", contentPolicy)
	// Validate the literal authority before routing; hostname parsing alone loses
	// the port and would admit DNS rebinding through an attacker's hostname.
	if string(c.Request().Host()) != Address {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if origin := c.Get(fiber.HeaderOrigin); origin != "" && origin != sourceOrigin {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if c.Get("Sec-Fetch-Site") == "cross-site" {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
		return c.SendStatus(fiber.StatusMethodNotAllowed)
	}
	return c.Next()
}

type asset struct {
	body        []byte
	contentType string
}

// Read only the isolated embedded bundle, once at startup. Requests perform an
// immutable map lookup and reuse Fiber's response buffer, with no filesystem I/O.
func loadAssets(files fs.FS) (map[string]asset, error) {
	assets := make(map[string]asset)
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = fiber.MIMEOctetStream
		}
		assets[name] = asset{body: body, contentType: contentType}
		return nil
	})
	return assets, err
}

func (s *Server) serveAsset(c fiber.Ctx) error {
	// Fiber expands "*" to "*1" with an allocation; use the canonical key.
	name := c.Params("*1")
	if name == "" {
		name = "index.html"
	}
	file, ok := s.assets[name]
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}
	c.Set(fiber.HeaderContentType, file.contentType)
	return c.Send(file.body)
}
