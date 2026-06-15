package rpc

import (
	"sync"

	"github.com/nats-io/nats.go"
)

// laneStore persists admin-side lane metadata (display aliases) in a NATS KV
// bucket, so a renamed lane keeps its label across restarts and is shared by
// all replicas. JetStream itself is the source of truth for the lanes
// themselves; this only holds the operator's cosmetic overrides.
type laneStore struct {
	js nats.JetStreamContext

	mu sync.Mutex
	kv nats.KeyValue
}

const laneBucket = "admin_lanes"

func newLaneStore(js nats.JetStreamContext) *laneStore { return &laneStore{js: js} }

// bucket lazily binds (creating on first use) the KV bucket.
func (s *laneStore) bucket() (nats.KeyValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kv != nil {
		return s.kv, nil
	}
	kv, err := s.js.KeyValue(laneBucket)
	if err == nats.ErrBucketNotFound {
		kv, err = s.js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:      laneBucket,
			History:     1,
			Description: "admin lane display aliases",
		})
	}
	if err != nil {
		return nil, err
	}
	s.kv = kv
	return kv, nil
}

// laneAliasKey is the KV key for a lane's alias. Consumer names are already
// dot-free (subject tokens), and stream names are uppercase, so stream.consumer
// is a stable, valid KV key.
func laneAliasKey(stream, consumer string) string { return stream + "." + consumer }

// aliases returns every stored alias keyed by stream.consumer. A missing bucket
// or empty store degrades to an empty map: aliases are cosmetic, never fatal.
func (s *laneStore) aliases() map[string]string {
	out := map[string]string{}
	kv, err := s.bucket()
	if err != nil {
		return out
	}
	keys, err := kv.Keys()
	if err != nil { // includes ErrNoKeysFound on an empty bucket
		return out
	}
	for _, k := range keys {
		e, err := kv.Get(k)
		if err != nil {
			continue
		}
		out[k] = string(e.Value())
	}
	return out
}

// setAlias stores (or, when alias is empty, clears) a lane's display alias.
func (s *laneStore) setAlias(stream, consumer, alias string) error {
	kv, err := s.bucket()
	if err != nil {
		return err
	}
	key := laneAliasKey(stream, consumer)
	if alias == "" {
		if err := kv.Delete(key); err != nil && err != nats.ErrKeyNotFound {
			return err
		}
		return nil
	}
	_, err = kv.PutString(key, alias)
	return err
}
