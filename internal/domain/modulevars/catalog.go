// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package modulevars shares the public module reply palettes with the custom
// command runtime. It contains no private module configuration.
package modulevars

import (
	"ItsBagelBot/pkg/codec"
	_ "embed"
)

//go:embed catalog.json
var catalog []byte

type Group struct {
	Name       string   `json:"name"`
	Fields     []string `json:"fields"`
	MessageKey string   `json:"message_key,omitempty"`
}

type Module struct {
	ID     string  `json:"id"`
	Groups []Group `json:"groups"`
}

func Catalog() []Module {
	var modules []Module
	if err := codec.Unmarshal(catalog, &modules); err != nil {
		panic(err)
	}
	return modules
}
