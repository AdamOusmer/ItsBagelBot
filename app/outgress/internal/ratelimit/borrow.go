package ratelimit

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/Yiling-J/theine-go"
	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"github.com/nats-io/nuid"
)

const (
	NeedStandard uint8 = 1 << iota
	NeedShared
	NeedSystem
	validNeeds        = NeedStandard | NeedShared | NeedSystem
	permitTTL         = 250 * time.Millisecond
	permitServiceName = "outgress-permit-v2"
)

type BorrowRequest struct {
	Version            uint16   `json:"version"`
	RequestID          string   `json:"request_id"`
	Epoch              uint64   `json:"epoch"`
	Generation         uint64   `json:"generation"`
	Bucket             BucketID `json:"bucket"`
	Need               uint8    `json:"need"`
	Profile            uint8    `json:"profile"`
	SharedRateMicros   int64    `json:"shared_rate_micros"`
	SharedBurst        int      `json:"shared_burst"`
	StandardRateMicros int64    `json:"standard_rate_micros,omitempty"`
	StandardBurst      int      `json:"standard_burst,omitempty"`
	DeadlineMS         int64    `json:"deadline_ms"`
}

type BorrowReply struct {
	Version     uint16 `json:"version"`
	Epoch       uint64 `json:"epoch"`
	GrantID     string `json:"grant_id,omitempty"`
	Paid        uint8  `json:"paid,omitempty"`
	RemainingMS int64  `json:"remaining_ms,omitempty"`
	Status      string `json:"status"` // granted, empty, stale, invalid
}

type cachedGrant struct {
	reply     BorrowReply
	expiresAt time.Time
}

type PermitService struct {
	nc      *nats.Conn
	service micro.Service
	dedupe  *theine.Cache[string, cachedGrant]
	grantor atomic.Pointer[LeaseManager]
	region  string
	podID   string
}

func NewPermitService(nc *nats.Conn, region, podID string, _ *BucketStore) (*PermitService, error) {
	if region == "" || podID == "" {
		return nil, errors.New("ratelimit: permit region and pod ID are required")
	}
	dedupe, err := theine.NewBuilder[string, cachedGrant](10_000).Build()
	if err != nil {
		return nil, err
	}
	service, err := micro.AddService(nc, micro.Config{
		Name: permitServiceName, Version: "3.0.0", Description: "Outgress peer permit service",
		Metadata: map[string]string{"pod_id": podID, "region": region},
	})
	if err != nil {
		dedupe.Close()
		return nil, err
	}
	permit := &PermitService{nc: nc, service: service, dedupe: dedupe, region: region, podID: podID}
	err = service.AddEndpoint("borrow", micro.HandlerFunc(permit.handleRequest),
		micro.WithEndpointSubject(permitSubject(region, podID)),
		micro.WithEndpointQueueGroupDisabled(),
		micro.WithEndpointPendingLimits(256, 1<<20),
	)
	if err != nil {
		_ = service.Stop()
		dedupe.Close()
		return nil, err
	}
	return permit, nil
}

func (ps *PermitService) SetGrantor(manager *LeaseManager) { ps.grantor.Store(manager) }

func (ps *PermitService) Close() {
	if ps.service != nil {
		_ = ps.service.Stop()
	}
	ps.dedupe.Close()
}

func permitSubject(region, podID string) string {
	return "bagel.outgress.permit.v2." + region + "." + podID
}

func (ps *PermitService) Borrow(ctx context.Context, donor Member, request BorrowRequest) (BorrowReply, error) {
	now := time.Now()
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(now.Add(permitTTL)) {
		deadline = now.Add(permitTTL)
	}
	request.RequestID = nuid.Next()
	request.DeadlineMS = deadline.UnixMilli()
	data, err := sonic.Marshal(&request)
	if err != nil {
		return BorrowReply{}, err
	}
	msg, err := ps.nc.RequestWithContext(ctx, permitSubject(donor.Region, donor.PodID), data)
	if err != nil {
		return BorrowReply{}, err
	}
	var reply BorrowReply
	if err := sonic.Unmarshal(msg.Data, &reply); err != nil {
		return BorrowReply{}, err
	}
	if reply.Version != planVersion || reply.Epoch != request.Epoch || reply.Paid&^request.Need != 0 {
		return BorrowReply{}, errors.New("ratelimit: invalid permit reply")
	}
	// Subtract the whole observed RTT. This is deliberately more conservative
	// than estimating one-way latency from unsynchronized clocks.
	if reply.Paid != 0 && time.Since(now) >= time.Duration(reply.RemainingMS)*time.Millisecond {
		return BorrowReply{}, context.DeadlineExceeded
	}
	return reply, nil
}

func (ps *PermitService) handleRequest(req micro.Request) {
	var borrow BorrowRequest
	if err := sonic.Unmarshal(req.Data(), &borrow); err != nil {
		_ = req.Error("400", "invalid request", nil)
		return
	}
	now := time.Now()
	if borrow.Version != planVersion || borrow.RequestID == "" || borrow.Bucket.Scope == "" ||
		borrow.Need == 0 || borrow.Need&^validNeeds != 0 || now.UnixMilli() >= borrow.DeadlineMS {
		ps.respond(req, BorrowReply{Version: planVersion, Epoch: borrow.Epoch, Status: "invalid"})
		return
	}
	if cached, ok := ps.dedupe.Get(borrow.RequestID); ok {
		remaining := time.Until(cached.expiresAt)
		if remaining <= 0 {
			cached.reply.Paid = 0
			cached.reply.Status = "stale"
			cached.reply.RemainingMS = 0
		} else {
			cached.reply.RemainingMS = remaining.Milliseconds()
		}
		ps.respond(req, cached.reply)
		return
	}

	reply := BorrowReply{Version: planVersion, Epoch: borrow.Epoch, Status: "invalid"}
	if grantor := ps.grantor.Load(); grantor != nil {
		reply = grantor.GrantPermit(now, borrow)
	}
	if reply.Paid != 0 {
		expiresAt := now.Add(permitTTL)
		reply.GrantID = nuid.Next()
		reply.RemainingMS = permitTTL.Milliseconds()
		ps.dedupe.SetWithTTL(borrow.RequestID, cachedGrant{reply: reply, expiresAt: expiresAt}, 1, permitTTL)
	}
	ps.respond(req, reply)
}

func (ps *PermitService) respond(req micro.Request, reply BorrowReply) {
	data, err := sonic.Marshal(&reply)
	if err != nil {
		_ = req.Error("500", "encode failure", nil)
		return
	}
	_ = req.Respond(data)
}

var (
	profileChatShared        = NewSpec(20, 20.0/30.0)
	profileChatStandard      = NewSpec(10, 10.0/30.0)
	profileChatModShared     = NewSpec(100, 100.0/30.0)
	profileChatModStandard   = NewSpec(50, 50.0/30.0)
	profileHelixShared       = NewSpec(700, 700.0/60.0)
	profileHelixStandard     = NewSpec(350, 350.0/60.0)
	profileHelixSystemShare  = NewSpec(100, 100.0/60.0)
	profileHelixUserShared   = NewSpec(800, 800.0/60.0)
	profileHelixUserStandard = NewSpec(400, 400.0/60.0)
)

func specsForProfile(profile uint8) (shared, standard Spec, ok bool) {
	switch profile {
	case profileChat:
		return profileChatShared, profileChatStandard, true
	case profileChatMod:
		return profileChatModShared, profileChatModStandard, true
	case profileHelixGeneral:
		return profileHelixShared, profileHelixStandard, true
	case profileHelixSystem:
		return profileHelixSystemShare, Spec{}, true
	case profileHelixUser:
		return profileHelixUserShared, profileHelixUserStandard, true
	default:
		return Spec{}, Spec{}, false
	}
}
