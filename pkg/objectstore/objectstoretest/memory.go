// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstoretest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"ItsBagelBot/pkg/objectstore"
)

type stored struct {
	info objectstore.Info
	body []byte
}

type Memory struct {
	mu      sync.Mutex
	objects map[string]stored
}

var _ objectstore.Store = (*Memory)(nil)

func NewMemory() *Memory { return &Memory{objects: map[string]stored{}} }

func (m *Memory) Put(_ context.Context, obj objectstore.Object) error {
	body, err := io.ReadAll(io.LimitReader(obj.Body, obj.Size+1))
	if err != nil {
		return err
	}
	if int64(len(body)) != obj.Size {
		return fmt.Errorf("objectstoretest: body is %d bytes, size is %d", len(body), obj.Size)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[obj.Key] = stored{
		info: objectstore.Info{Key: obj.Key, Size: obj.Size, ContentType: obj.ContentType, CacheControl: obj.CacheControl},
		body: body,
	}
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	o, err := m.lookup(key)
	if err != nil {
		return nil, objectstore.Info{}, err
	}
	return io.NopCloser(bytes.NewReader(o.body)), o.info, nil
}

func (m *Memory) Head(_ context.Context, key string) (objectstore.Info, error) {
	o, err := m.lookup(key)
	return o.info, err
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) DeletePrefix(_ context.Context, prefix string) error {
	if prefix == "" {
		return fmt.Errorf("objectstoretest: delete prefix must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			delete(m.objects, key)
		}
	}
	return nil
}

func (m *Memory) Copy(_ context.Context, src, dst string) error {
	o, err := m.lookup(src)
	if err != nil {
		return err
	}
	o.info.Key = dst
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[dst] = o
	return nil
}

func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objects))
	for k := range m.objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (m *Memory) lookup(key string) (stored, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return stored{}, fmt.Errorf("%w: %s", objectstore.ErrNotFound, key)
	}
	return o, nil
}
