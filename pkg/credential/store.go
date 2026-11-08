// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package credential isolates the native OS credential store. It never falls
// back to plaintext files or exposes implementation errors containing secrets.
package credential

import (
	"encoding/base64"
	"errors"

	vendor "github.com/zalando/go-keyring"
)

var (
	ErrNotFound    = errors.New("credential: not found")
	ErrUnavailable = errors.New("credential: secure OS store unavailable")
	ErrInvalid     = errors.New("credential: invalid item")
)

// The Darwin backend uses a bounded interactive security command; the double
// base64 representation plus validated names stays below its 4096-byte limit.
const MaxBytes = 2048

type backend struct {
	get func(string, string) (string, error)
	set func(string, string, string) error
	del func(string, string) error
}
type Store struct {
	service string
	backend backend
}

func New(service string) (*Store, error) {
	if !validName(service) {
		return nil, ErrInvalid
	}
	return &Store{service: service, backend: backend{get: vendor.Get, set: vendor.Set, del: vendor.Delete}}, nil
}
func validName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i := range len(value) {
		if !nameByte(value[i]) {
			return false
		}
	}
	return true
}
func nameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || c == '/'
}

// Read returns owned bytes. Encoding keeps binary private keys portable across
// OS string stores, but is not an additional encryption layer.
func (s *Store) Read(name string) ([]byte, error) {
	if s == nil || !validName(name) {
		return nil, ErrInvalid
	}
	value, err := s.backend.get(s.service, name)
	if errors.Is(err, vendor.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(value) > base64.StdEncoding.EncodedLen(MaxBytes) {
		return nil, ErrInvalid
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > MaxBytes {
		return nil, ErrInvalid
	}
	return raw, nil
}
func (s *Store) Write(name string, raw []byte) error {
	if s == nil || !validName(name) || len(raw) == 0 || len(raw) > MaxBytes {
		return ErrInvalid
	}
	if s.backend.set(s.service, name, base64.StdEncoding.EncodeToString(raw)) != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Store) Delete(name string) error {
	if s == nil || !validName(name) {
		return ErrInvalid
	}
	err := s.backend.del(s.service, name)
	if err == nil || errors.Is(err, vendor.ErrNotFound) {
		return nil
	}
	return ErrUnavailable
}
