// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package confcache_test

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"ItsBagelBot/app/twitch/sesame/confcache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countingParse(calls *atomic.Int64) func([]byte) string {
	return func(raw []byte) string {
		calls.Add(1)
		return string(raw)
	}
}

func TestGetParsesOnlyWhenTheBlobChanges(t *testing.T) {
	tests := []struct {
		name       string
		reads      []string
		wantParses int64
	}{
		{"parses a repeated blob once", []string{`{"level":"strict"}`, `{"level":"strict"}`, `{"level":"strict"}`}, 1},
		{"keeps distinct blobs apart", []string{`{"level":"strict"}`, `{"level":"none"}`, `{"level":"none"}`, `{"level":"strict"}`}, 2},
		{"reparses an edited blob", []string{`{"rules":"a => 1"}`, `{"rules":"a => 2"}`}, 2},
		{"never caches an empty blob", []string{"", ""}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int64
			c := confcache.New[string]()
			parse := countingParse(&calls)

			for _, blob := range tt.reads {
				require.Equal(t, blob, c.Get([]byte(blob), parse))
			}

			assert.Equal(t, tt.wantParses, calls.Load())
		})
	}
}

func TestEvictionDropsTheColdestBlobAndKeepsTheRecentlyUsedOne(t *testing.T) {
	const hot, flood = "hot-config", 4096
	var calls atomic.Int64
	c := confcache.New[string]()
	parse := countingParse(&calls)

	c.Get([]byte(hot), parse)
	for i := 0; i < flood; i++ {
		c.Get([]byte(strconv.Itoa(i)), parse)
		c.Get([]byte(hot), parse)
	}
	assert.Equal(t, int64(flood+1), calls.Load(), "hot blob must survive every eviction")

	c.Get([]byte("0"), parse)
	assert.Equal(t, int64(flood+2), calls.Load(), "coldest blob must have been evicted")

	c.Get([]byte(strconv.Itoa(flood-1)), parse)
	assert.Equal(t, int64(flood+2), calls.Load(), "most recent blob must still be cached")
}

func TestConcurrentMissesOnTheSameBlobShareOneEntry(t *testing.T) {
	var calls atomic.Int64
	var rendezvous sync.WaitGroup
	rendezvous.Add(2)
	c := confcache.New[string]()
	parse := func(raw []byte) string {
		calls.Add(1)
		rendezvous.Done()
		rendezvous.Wait()
		return string(raw)
	}

	var wg sync.WaitGroup
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.Equal(t, "blob", c.Get([]byte("blob"), parse))
		}()
	}
	wg.Wait()
	c.Get([]byte("blob"), parse)

	assert.Equal(t, int64(2), calls.Load())
}

func TestConcurrentGetIsSafe(t *testing.T) {
	var calls atomic.Int64
	c := confcache.New[string]()
	parse := countingParse(&calls)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				blob := fmt.Sprintf(`{"c":%d}`, i%32)
				require.Equal(t, blob, c.Get([]byte(blob), parse))
			}
		}()
	}
	wg.Wait()
}
