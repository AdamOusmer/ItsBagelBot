// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package templates

import (
	"embed"
	"io/fs"
	"slices"
	"strings"

	"ItsBagelBot/internal/domain/overlays"
)

//go:embed *.json
var files embed.FS

// Keys lists the starter documents by the key add_template takes.
func Keys() []string {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		panic("templates: " + err.Error())
	}
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = strings.TrimSuffix(entry.Name(), ".json")
	}
	slices.Sort(keys)
	return keys
}

// Lookup parses and validates the starter document for key.
func Lookup(key string) (overlays.Document, bool, error) {
	raw, err := files.ReadFile(key + ".json")
	if err != nil {
		return overlays.Document{}, false, nil
	}
	doc, err := overlays.Parse(raw)
	return doc, true, err
}
