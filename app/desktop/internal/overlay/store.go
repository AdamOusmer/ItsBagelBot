// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlay

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"ItsBagelBot/pkg/codec"
)

// Escaping can grow a validated 256 KiB input up to sixfold when persisted.
const maxSavedBytes = 2 << 20

type saved struct {
	Token string `json:"token"`
	Scene Scene  `json:"scene"`
}

type sceneStore interface {
	load() (saved, error)
	save(saved) error
}

type fileStore struct{ path string }

func (f fileStore) load() (saved, error) {
	var state saved
	file, err := os.Open(f.path)
	if err != nil {
		return state, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSavedBytes+1))
	if err != nil {
		return state, err
	}
	if len(data) > maxSavedBytes {
		return state, errors.New("saved overlay exceeds 2 MiB")
	}
	err = codec.UnmarshalStrict(data, &state)
	return state, err
}

func (f fileStore) save(state saved) error {
	data, err := codec.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > maxSavedBytes {
		return errors.New("saved overlay exceeds 2 MiB")
	}
	file, err := os.CreateTemp(filepath.Dir(f.path), ".overlay-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, f.path)
}
