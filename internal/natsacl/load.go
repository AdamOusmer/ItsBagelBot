// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func LoadACL(path string) (*ACL, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("natsacl: read %s: %w", path, err)
	}
	return ParseACL(data)
}

func ParseACL(data []byte) (*ACL, error) {
	var acl ACL
	if err := decodeStrict(data, &acl); err != nil {
		return nil, fmt.Errorf("natsacl: parse accounts.yaml: %w", err)
	}
	return &acl, nil
}

func LoadKeys(path string) (*Keys, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("natsacl: read %s: %w", path, err)
	}
	return ParseKeys(data)
}

func ParseKeys(data []byte) (*Keys, error) {
	var keys Keys
	if err := decodeStrict(data, &keys); err != nil {
		return nil, fmt.Errorf("natsacl: parse accounts.keys.yaml: %w", err)
	}
	return &keys, nil
}

func decodeStrict(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(v)
}
