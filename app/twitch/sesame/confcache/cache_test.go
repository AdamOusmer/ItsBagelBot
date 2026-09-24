// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package confcache

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countingParse(calls *atomic.Int64) func([]byte) string {
	return func(raw []byte) string {
		calls.Add(1)
		return string(raw)
	}
}

func TestGetParsesOncePerBlob(t *testing.T) {
	var calls atomic.Int64
	c := New[string]()
	parse := countingParse(&calls)

	for i := 0; i < 10; i++ {
		assert.Equal(t, `{"level":"strict"}`, c.Get([]byte(`{"level":"strict"}`), parse))
	}

	assert.Equal(t, int64(1), calls.Load(), "repeat reads of the same blob must not re-parse")
}

func TestDistinctBlobsDoNotCollide(t *testing.T) {
	var calls atomic.Int64
	c := New[string]()
	parse := countingParse(&calls)

	const (
		chanA = `{"level":"strict","block_terms":"alpha"}`
		chanB = `{"level":"none","block_terms":"beta"}`
	)
	assert.Equal(t, chanA, c.Get([]byte(chanA), parse))
	assert.Equal(t, chanB, c.Get([]byte(chanB), parse))
	assert.Equal(t, chanB, c.Get([]byte(chanB), parse))
	assert.Equal(t, chanA, c.Get([]byte(chanA), parse))

	assert.Equal(t, int64(2), calls.Load())
	assert.Equal(t, 2, c.len())
}

func TestGetSeesEditedBlob(t *testing.T) {
	var calls atomic.Int64
	c := New[string]()
	parse := countingParse(&calls)

	assert.Equal(t, `{"rules":"a => 1"}`, c.Get([]byte(`{"rules":"a => 1"}`), parse))
	assert.Equal(t, `{"rules":"a => 2"}`, c.Get([]byte(`{"rules":"a => 2"}`), parse))
	assert.Equal(t, int64(2), calls.Load())
}

func TestEmptyBlobIsNotCached(t *testing.T) {
	var calls atomic.Int64
	c := New[string]()
	parse := countingParse(&calls)

	assert.Equal(t, "", c.Get(nil, parse))
	assert.Equal(t, "", c.Get([]byte{}, parse))

	assert.Equal(t, int64(2), calls.Load())
	assert.Equal(t, 0, c.len())
}

func TestEvictionHoldsTheCap(t *testing.T) {
	var calls atomic.Int64
	c := New[string]()
	parse := countingParse(&calls)

	for i := 0; i < maxEntries*2; i++ {
		c.Get([]byte(strconv.Itoa(i)), parse)
	}

	assert.Equal(t, maxEntries, c.len(), "cache must not grow past its cap")
	assert.Equal(t, int64(maxEntries*2), calls.Load())
}

func TestEvictionIsLeastRecentlyUsed(t *testing.T) {
	var calls atomic.Int64
	c := New[string]()
	parse := countingParse(&calls)

	const hot = "hot-config"
	c.Get([]byte(hot), parse)
	for i := 0; i < maxEntries-1; i++ {
		c.Get([]byte(strconv.Itoa(i)), parse)
		c.Get([]byte(hot), parse)
	}
	before := calls.Load()

	c.Get([]byte("overflow"), parse)
	c.Get([]byte(hot), parse)
	assert.Equal(t, before+1, calls.Load(), "hot blob must have survived eviction")

	c.Get([]byte("0"), parse)
	assert.Equal(t, before+2, calls.Load(), "coldest blob must have been evicted")
}

func TestConcurrentGetIsSafe(t *testing.T) {
	var calls atomic.Int64
	c := New[string]()
	parse := countingParse(&calls)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				blob := fmt.Sprintf(`{"c":%d}`, i%32)
				require.Equal(t, blob, c.Get([]byte(blob), parse))
			}
		}(g)
	}
	wg.Wait()

	assert.Equal(t, 32, c.len())
}
