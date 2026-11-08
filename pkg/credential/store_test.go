// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package credential

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	vendor "github.com/zalando/go-keyring"
)

func TestReadDecodedBounds(t *testing.T) {
	store, _ := New("ItsBagelBot.desktop")
	for _, size := range []int{0, MaxBytes, MaxBytes + 1} {
		encoded := base64.StdEncoding.EncodeToString(make([]byte, size))
		store.backend.get = func(string, string) (string, error) { return encoded, nil }
		raw, err := store.Read("account")
		if size == 0 || size > MaxBytes {
			if err != ErrInvalid {
				t.Fatal("accepted empty or oversized decoded credential")
			}
			continue
		}
		if err != nil || len(raw) != size {
			t.Fatal("rejected credential at the decoded bound", err)
		}
	}
}

func TestOwnedBinaryRoundTrip(t *testing.T) {
	values := map[string]string{}
	s, _ := New("ItsBagelBot.desktop")
	s.backend = backend{get: func(service, name string) (string, error) {
		value, ok := values[service+name]
		if !ok {
			return "", vendor.ErrNotFound
		}
		return value, nil
	}, set: func(service, name, value string) error { values[service+name] = value; return nil }, del: func(service, name string) error { delete(values, service+name); return nil }}
	if _, err := s.Read("account"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	value := []byte{0, 1, 128, 255}
	if err := s.Write("account", value); err != nil {
		t.Fatal(err)
	}
	first, err := s.Read("account")
	if err != nil {
		t.Fatal(err)
	}
	first[0] = 99
	second, err := s.Read("account")
	if err != nil || string(second) != string(value) {
		t.Fatal("storage aliased decoded bytes")
	}
	if err = s.Delete("account"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read("account"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestStoreFailureNeverLeaksBackendDetails(t *testing.T) {
	s, _ := New("ItsBagelBot.desktop")
	secretError := errors.New("secret implementation detail")
	s.backend = backend{get: func(string, string) (string, error) { return "", secretError }, set: func(string, string, string) error { return secretError }, del: func(string, string) error { return secretError }}
	_, err := s.Read("account")
	if err != ErrUnavailable {
		t.Fatal(err)
	}
	if err = s.Write("account", []byte("secret")); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err = s.Delete("account"); err != ErrUnavailable {
		t.Fatal(err)
	}
}
func TestStoreBoundsAndNames(t *testing.T) {
	for _, name := range []string{"", "bad\nname", "bad'name", strings.Repeat("a", 129)} {
		if _, err := New(name); err != ErrInvalid {
			t.Fatal("accepted invalid namespace")
		}
	}
	s, _ := New("ItsBagelBot.desktop")
	if err := s.Write("account", make([]byte, MaxBytes+1)); err != ErrInvalid {
		t.Fatal(err)
	}
	s.backend.get = func(string, string) (string, error) { return "malformed!", nil }
	if _, err := s.Read("account"); err != ErrInvalid {
		t.Fatal(err)
	}
}
