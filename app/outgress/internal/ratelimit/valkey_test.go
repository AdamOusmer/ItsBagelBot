package ratelimit

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"
)

func TestNewSpecPrecomputesArguments(t *testing.T) {
	spec := NewSpec(20, 20.0/30.0)
	if spec.capacityArg != "20" {
		t.Fatalf("capacity = %q, want 20", spec.capacityArg)
	}
	if spec.refillArg == "" {
		t.Fatal("refill argument is empty")
	}
	if spec.ttlArg != "60" {
		t.Fatalf("ttl = %q, want 60", spec.ttlArg)
	}
}

func TestNewSpecRejectsInvalidConfiguration(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewSpec did not panic")
		}
	}()
	_ = NewSpec(0, 1)
}

// This test is opt-in because the script's atomicity and server-TIME behavior
// need a real Valkey interpreter, not a command mock. Run with VALKEY_TEST_ADDR.
func TestAllowOrderedIntegration(t *testing.T) {
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{address},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prefix := "test:outgress:limiter:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	firstKey, secondKey := prefix+":first", prefix+":second"
	defer client.Do(context.Background(), client.B().Del().Key(firstKey, secondKey).Build())

	limiter := New(client)
	spec := NewSpec(2, 0.001)
	first, second := spec.ForKey(firstKey), spec.ForKey(secondKey)

	denied, err := limiter.AllowOrdered(ctx, first, second)
	if err != nil || denied != 0 {
		t.Fatalf("fresh pair denied/error = %d/%v", denied, err)
	}

	// An empty first bucket must not touch the second bucket at all.
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	if err := client.Do(ctx, client.B().Hset().Key(firstKey).FieldValue().
		FieldValue("tokens", "0").FieldValue("last_ms", future).Build()).Error(); err != nil {
		t.Fatal(err)
	}
	before, err := client.Do(ctx, client.B().Hgetall().Key(secondKey).Build()).AsStrMap()
	if err != nil {
		t.Fatal(err)
	}
	denied, err = limiter.AllowOrdered(ctx, first, second)
	if err != nil || denied != 1 {
		t.Fatalf("first-empty pair denied/error = %d/%v", denied, err)
	}
	after, err := client.Do(ctx, client.B().Hgetall().Key(secondKey).Build()).AsStrMap()
	if err != nil {
		t.Fatal(err)
	}
	if before["tokens"] != after["tokens"] || before["last_ms"] != after["last_ms"] {
		t.Fatalf("second bucket changed after first denial: before=%v after=%v", before, after)
	}

	// An atomic evaluation means if the second bucket is empty, the first token is NOT consumed.
	for key, tokens := range map[string]string{firstKey: "2", secondKey: "0"} {
		if err := client.Do(ctx, client.B().Hset().Key(key).FieldValue().
			FieldValue("tokens", tokens).FieldValue("last_ms", future).Build()).Error(); err != nil {
			t.Fatal(err)
		}
	}
	denied, err = limiter.AllowOrdered(ctx, first, second)
	if err != nil || denied != 2 {
		t.Fatalf("second-empty pair denied/error = %d/%v", denied, err)
	}
	tokens, err := client.Do(ctx, client.B().Hget().Key(firstKey).Field("tokens").Build()).ToString()
	if err != nil {
		t.Fatal(err)
	}
	if tokens != "2" {
		t.Fatalf("first tokens = %q, want 2 (atomic fallback)", tokens)
	}

	// The script reads both keys before writing either one. A wrong-type second
	// key must therefore fail without consuming the first token.
	if err := client.Do(ctx, client.B().Hset().Key(firstKey).FieldValue().
		FieldValue("tokens", "2").FieldValue("last_ms", future).Build()).Error(); err != nil {
		t.Fatal(err)
	}
	if err := client.Do(ctx, client.B().Set().Key(secondKey).Value("wrong-type").Build()).Error(); err != nil {
		t.Fatal(err)
	}
	if _, err := limiter.AllowOrdered(ctx, first, second); err == nil {
		t.Fatal("wrong-type second bucket did not fail")
	}
	tokens, err = client.Do(ctx, client.B().Hget().Key(firstKey).Field("tokens").Build()).ToString()
	if err != nil {
		t.Fatal(err)
	}
	if tokens != "2" {
		t.Fatalf("first tokens after second-key error = %q, want 2", tokens)
	}
}
