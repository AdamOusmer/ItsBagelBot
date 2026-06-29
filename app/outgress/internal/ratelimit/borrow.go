package ratelimit

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Yiling-J/theine-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

const (
	NeedStandard uint8 = 1 << iota
	NeedShared
	NeedSystem
)

type BorrowRequest struct {
	Version    uint16 `json:"version"`
	RequestID  string `json:"request_id"`
	Epoch      uint64 `json:"epoch"`
	Bucket     string `json:"bucket"`
	Need       uint8  `json:"need"`
	DeadlineMS int64  `json:"deadline_ms"`
}

type BorrowReply struct {
	Version     uint16 `json:"version"`
	Epoch       uint64 `json:"epoch"`
	GrantID     string `json:"grant_id,omitempty"`
	Paid        uint8  `json:"paid,omitempty"`
	RemainingMS int64  `json:"remaining_ms,omitempty"`
	Status      string `json:"status"` // granted, partial, empty, stale, invalid
}

type PermitService struct {
	service micro.Service
	buckets *theine.Cache[string, *LocalBucket]
	dedupe  *theine.Cache[string, BorrowReply]
}

func NewPermitService(nc *nats.Conn, region, podID string, buckets *theine.Cache[string, *LocalBucket]) (*PermitService, error) {
	dedupe, err := theine.NewBuilder[string, BorrowReply](10000).Build()
	if err != nil {
		return nil, err
	}

	ps := &PermitService{
		buckets: buckets,
		dedupe:  dedupe,
	}

	subject := fmt.Sprintf("bagel.outgress.permit.v1.%s.%s", region, podID)

	config := micro.Config{
		Name:        "outgress-permit-" + podID,
		Version:     "1.0.0",
		Description: "Outgress peer permit service",
		Endpoint: &micro.EndpointConfig{
			Subject: subject,
			Handler: micro.HandlerFunc(ps.handleRequest),
		},
	}

	svc, err := micro.AddService(nc, config)
	if err != nil {
		return nil, err
	}
	ps.service = svc

	return ps, nil
}

func (ps *PermitService) Close() {
	if ps.service != nil {
		ps.service.Stop()
	}
	ps.dedupe.Close()
}

func (ps *PermitService) handleRequest(req micro.Request) {
	var br BorrowRequest
	if err := json.Unmarshal(req.Data(), &br); err != nil {
		req.Error("400", "invalid request", nil)
		return
	}

	if br.Version != 1 {
		reply := BorrowReply{Version: 1, Status: "invalid"}
		data, _ := json.Marshal(reply)
		req.Respond(data)
		return
	}

	now := time.Now()
	nowMS := now.UnixMilli()
	
	if nowMS > br.DeadlineMS {
		reply := BorrowReply{Version: 1, Status: "stale"}
		data, _ := json.Marshal(reply)
		req.Respond(data)
		return
	}

	// Dedupe cache check
	if cached, ok := ps.dedupe.Get(br.RequestID); ok {
		// Update remaining ms if we are serving from cache
		if cached.RemainingMS > 0 {
			cached.RemainingMS = br.DeadlineMS - nowMS
		}
		data, _ := json.Marshal(cached)
		req.Respond(data)
		return
	}

	reply := BorrowReply{
		Version: 1,
		Epoch:   br.Epoch,
		Status:  "empty",
	}

	bucket, ok := ps.buckets.Get(br.Bucket)
	if ok && bucket.Epoch() == br.Epoch && bucket.IsValid(now) {
		var paid uint8
		
		// Attempt to satisfy Need Standard then Shared then System
		// Attempt to satisfy Need Standard then Shared then System
		if br.Need&NeedStandard != 0 && br.Need&NeedShared != 0 {
			st, sh := bucket.TryStandard(now)
			// Only grant if both were successfully paid, as standard requires both.
			if st && sh {
				paid |= NeedStandard
				paid |= NeedShared
			}
		} else if br.Need&NeedStandard != 0 {
			// Actually standard can only be tried with shared in our design, 
			// but if they ask for just standard we can technically still try
			// Note: LocalBucket has no TryStandardOnly, so we do not blindly call TryStandard
			// to avoid wasting shared tokens.
		} else if br.Need&NeedShared != 0 {
			if bucket.TryPremium(now) {
				paid |= NeedShared
			}
		}

		if br.Need&NeedSystem != 0 {
			// System is just TryPremium logic essentially
			if bucket.TryPremium(now) {
				paid |= NeedSystem
			}
		}

		reply.Paid = paid
		if paid == br.Need {
			reply.Status = "granted"
		} else if paid > 0 {
			reply.Status = "partial"
		}
	} else if ok {
		reply.Status = "stale"
	} else {
		reply.Status = "invalid" // don't own bucket
	}

	reply.GrantID = fmt.Sprintf("grant-%d", now.UnixNano())
	reply.RemainingMS = br.DeadlineMS - nowMS

	ps.dedupe.SetWithTTL(br.RequestID, reply, 1, 10*time.Second)

	data, _ := json.Marshal(reply)
	req.Respond(data)
}
