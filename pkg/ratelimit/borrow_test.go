package ratelimit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
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

	store := NewBucketStore(100)
	service, err := NewPermitService(nc, "local", "pod-a", store)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	manager := NewLeaseManager(nil, store, service, WithLeaseIdentity("local", "pod-a"))
	service.SetGrantor(manager)

	now := time.Now()
	plan := Plan{
		Version: planVersion, Epoch: 1, Generation: 7,
		ValidFromMS: now.Add(-time.Second).UnixMilli(), ValidUntilMS: now.Add(time.Hour).UnixMilli(),
		Members: []Member{{PodID: "pod-a", Region: "local"}},
	}
	if err := plan.ComputeDigest(); err != nil {
		t.Fatal(err)
	}
	if err := manager.ActivatePlan(plan, now, now, 0); err != nil {
		t.Fatal(err)
	}

	sharedRate, sharedBurst := localShare(profileHelixShared, 1, 0)
	request := BorrowRequest{
		Version: planVersion, RequestID: "req-1", Epoch: 1, Generation: 7,
		Bucket: BucketID{Scope: "helix:app"}, Need: NeedShared, Profile: profileHelixGeneral,
		SharedRateMicros: limitMicros(sharedRate), SharedBurst: sharedBurst,
		DeadlineMS: time.Now().Add(time.Second).UnixMilli(),
	}
	// First request creates an empty share. Let it earn a token, then use a
	// stable request ID to verify lender-side deduplication.
	_ = manager.GrantPermit(time.Now(), request)
	time.Sleep(100 * time.Millisecond)
	data, err := sonic.Marshal(&request)
	if err != nil {
		t.Fatal(err)
	}
	message, err := nc.Request(permitSubject("local", "pod-a"), data, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var first BorrowReply
	if err := sonic.Unmarshal(message.Data, &first); err != nil {
		t.Fatal(err)
	}
	if first.Status != "granted" || first.Paid != NeedShared || first.GrantID == "" {
		t.Fatalf("unexpected first reply: %+v", first)
	}

	message, err = nc.Request(permitSubject("local", "pod-a"), data, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var duplicate BorrowReply
	if err := sonic.Unmarshal(message.Data, &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.GrantID != first.GrantID {
		t.Fatalf("duplicate grant = %q, want %q", duplicate.GrantID, first.GrantID)
	}

	// Also cover the client wrapper and reply validation with a new request.
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request.RequestID = ""
	reply, err := service.Borrow(ctx, plan.Members[0], request)
	if err != nil || reply.Paid != NeedShared {
		t.Fatalf("Borrow() = %+v, %v", reply, err)
	}
}
