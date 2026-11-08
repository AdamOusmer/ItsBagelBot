// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlay

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"ItsBagelBot/pkg/codec"
)

type Snapshot struct {
	Revision uint64 `json:"revision"`
	Scene    Scene  `json:"scene"`
}

// A publication is immutable after Store. Encoding and allocation happen once on
// the desktop write path, never for each OBS reader. Only changed is closed.
type publication struct {
	state    saved
	revision uint64
	cursor   string
	bearer   string
	wire     []byte
	changed  chan struct{}
}

func (p *publication) authorized(header string) bool {
	return subtle.ConstantTimeCompare([]byte(header), []byte(p.bearer)) == 1
}

// Source owns durable scenes and immutable reader publications, independently of HTTP.
type Source struct {
	mu      sync.Mutex
	store   sceneStore
	current atomic.Pointer[publication]
}

func OpenSource(dir string) (*Source, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return openSource(fileStore{path: filepath.Join(dir, "overlay.json")})
}

func openSource(store sceneStore) (*Source, error) {
	state, err := store.load()
	if errors.Is(err, os.ErrNotExist) {
		state, err = initialState(store)
	}
	if err != nil {
		return nil, err
	}
	token, err := hex.DecodeString(state.Token)
	if err != nil || len(token) != 32 {
		return nil, errors.New("invalid overlay credentials")
	}
	if err = state.Scene.validate(); err != nil {
		return nil, err
	}
	state.Scene.normalize()
	current, err := preparePublication(state, 1)
	if err != nil {
		return nil, err
	}
	source := &Source{store: store}
	source.current.Store(current)
	return source, nil
}

func initialState(store sceneStore) (saved, error) {
	token, err := newToken()
	if err != nil {
		return saved{}, err
	}
	state := saved{Token: token, Scene: Scene{Resolution: "1080p", Layers: []Layer{}}}
	return state, store.save(state)
}

func newToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func preparePublication(state saved, revision uint64) (*publication, error) {
	wire, err := codec.Marshal(Snapshot{Revision: revision, Scene: state.Scene})
	if err != nil {
		return nil, err
	}
	return &publication{state: state, revision: revision, cursor: strconv.FormatUint(revision, 10),
		bearer: "Bearer " + state.Token, wire: wire, changed: make(chan struct{})}, nil
}

func (s *Source) SourceURL() string { return "http://" + Address + "/#" + s.current.Load().state.Token }
func (s *Source) Current() Scene    { return s.current.Load().state.Scene.clone() }

func (s *Source) Publish(raw string) error {
	scene, err := parseScene(raw)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit(saved{Token: s.current.Load().state.Token, Scene: scene})
}

func (s *Source) ResetSourceURL() (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.commit(saved{Token: token, Scene: s.current.Load().state.Scene}); err != nil {
		return "", err
	}
	return s.SourceURL(), nil
}

// Called with mu held. Readers observe the new immutable publication only after
// durable persistence succeeds; failed writes neither advance nor wake them.
func (s *Source) commit(state saved) error {
	previous := s.current.Load()
	next, err := preparePublication(state, previous.revision+1)
	if err != nil {
		return err
	}
	if err = s.store.save(state); err != nil {
		return err
	}
	s.current.Store(next)
	close(previous.changed)
	return nil
}
