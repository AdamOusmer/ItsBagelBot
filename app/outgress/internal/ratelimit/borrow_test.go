package ratelimit

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Yiling-J/theine-go"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestPermitService(t *testing.T) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		t.Skip("NATS_URL is not set")
	}

	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	cache, err := theine.NewBuilder[string, *LocalBucket](100).Build()
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	b := NewLocalBucket()
	now := time.Now()
	b.Update(now, 1, "pod-a", now.Add(-time.Second), now.Add(time.Hour), rate.Limit(10), 10, rate.Limit(5), 5)
	
	// Wait a bit to fill up
	time.Sleep(100 * time.Millisecond)

	cache.Set("test-bucket", b, 1)

	svc, err := NewPermitService(nc, "us-east-1", "pod-a", cache)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	// Make a request
	req := BorrowRequest{
		Version:    1,
		RequestID:  "req-1",
		Epoch:      1,
		Bucket:     "test-bucket",
		Need:       NeedShared,
		DeadlineMS: time.Now().Add(time.Second).UnixMilli(),
	}
	
	data, _ := json.Marshal(req)
	msg, err := nc.Request(fmt.Sprintf("bagel.outgress.permit.v1.us-east-1.pod-a"), data, 2*time.Second)
	assert.NoError(t, err)

	var reply BorrowReply
	err = json.Unmarshal(msg.Data, &reply)
	assert.NoError(t, err)
	assert.Equal(t, uint16(1), reply.Version)
	assert.Equal(t, "granted", reply.Status)
	assert.Equal(t, NeedShared, reply.Paid)

	// Test dedupe cache
	msg2, err := nc.Request(fmt.Sprintf("bagel.outgress.permit.v1.us-east-1.pod-a"), data, 2*time.Second)
	assert.NoError(t, err)

	var reply2 BorrowReply
	err = json.Unmarshal(msg2.Data, &reply2)
	assert.NoError(t, err)
	assert.Equal(t, reply.GrantID, reply2.GrantID) // Must match exactly
}
