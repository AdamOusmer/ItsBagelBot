// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/enttest"
	"ItsBagelBot/app/db/transactions/tebex"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func requestBasket(t *testing.T, nc *nats.Conn, req transactionsrpc.BasketCreateRequest) transactionsrpc.BasketCreateReply {
	t.Helper()
	payload, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := nc.Request("checkout.basket_create", payload, 3*time.Second)
	require.NoError(t, err)
	var reply transactionsrpc.BasketCreateReply
	require.NoError(t, codec.Unmarshal(msg.Data, &reply))
	return reply
}

type checkoutProviderCall struct {
	path      string
	body      map[string]any
	leaseHeld bool
}

func checkoutProvider(t *testing.T, db *ent.Client, userID uint64, failPackage bool) (*tebex.Client, <-chan checkoutProviderCall) {
	t.Helper()
	calls := make(chan checkoutProviderCall, 8)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := codec.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		lease, err := db.GiveawayUserLease.Get(r.Context(), fmt.Sprintf("user:%d", userID))
		call := checkoutProviderCall{r.URL.Path, body, err == nil && lease.LeaseUntil.After(time.Now())}
		select {
		case calls <- call:
		default:
			t.Error("unexpected extra provider request")
		}
		if failPackage && r.URL.Path == "/api/baskets/basket/packages" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ident":"basket","links":{"checkout":"https://checkout.example/basket"}}}`))
	}))
	t.Cleanup(provider.Close)
	client, err := tebex.New(tebex.Config{WebstoreToken: "store", PrivateKey: "private", PackageID: 42, BaseURL: provider.URL})
	require.NoError(t, err)
	return client, calls
}

func requireCheckoutLeaseState(t *testing.T, guard *CheckoutGuard, db *ent.Client, held bool) {
	t.Helper()
	lease, err := db.GiveawayUserLease.Get(context.Background(), "user:7")
	require.NoError(t, err)
	require.Equal(t, held, lease.LeaseUntil.After(time.Now()), "only a preexisting owner's lease should remain held")
	if held {
		return
	}
	release, err := guard.Lease.AcquireUserLease(context.Background(), 7)
	require.NoError(t, err, "released lease can be reacquired immediately")
	release()
}

type checkoutCoverageFunc func(context.Context, uint64) (usersrpc.PremiumCoverage, error)

func (f checkoutCoverageFunc) Coverage(ctx context.Context, id uint64) (usersrpc.PremiumCoverage, error) {
	return f(ctx, id)
}

type checkoutBasketCase struct {
	name          string
	coverage      fakeCoverage
	award         bool
	heldLease     bool
	providerFails bool
	wantCode      domainrpc.Code
	wantError     string
	wantCalls     int
	skipCode      bool
}

func checkoutBasketGuard(t *testing.T, db *ent.Client, tc checkoutBasketCase) (*CheckoutGuard, <-chan bool) {
	t.Helper()
	ctx := context.Background()
	coverageChecks := make(chan bool, 8)
	guard := NewCheckoutGuard(db, checkoutCoverageFunc(func(ctx context.Context, id uint64) (usersrpc.PremiumCoverage, error) {
		lease, err := db.GiveawayUserLease.Get(ctx, "user:7")
		coverageChecks <- id == 7 && err == nil && lease.LeaseUntil.After(time.Now())
		return tc.coverage.Coverage(ctx, id)
	}))
	if tc.award {
		_, err := db.GiveawayAward.Create().SetID("award").SetGiveawayID("campaign").SetDrawID("draw").SetUserID(7).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("calendar-month").Save(ctx)
		require.NoError(t, err)
	}
	if tc.heldLease {
		release, err := guard.Lease.AcquireUserLease(ctx, 7)
		require.NoError(t, err)
		t.Cleanup(release)
	}
	return guard, coverageChecks
}

func requireCheckoutCoverageChecks(t *testing.T, coverageChecks <-chan bool, tc checkoutBasketCase) {
	t.Helper()
	if tc.award || tc.heldLease {
		require.Empty(t, coverageChecks, "lease acquisition and award checks must precede users coverage reads")
	} else {
		require.Len(t, coverageChecks, 1)
		require.True(t, <-coverageChecks, "coverage must be checked under the buyer's lease")
	}
}

func TestCheckoutRPCBasketLeaseLifecycle(t *testing.T) {
	for _, tc := range []checkoutBasketCase{
		{name: "eligible buyer", wantCalls: 2},
		{name: "package addition fails", providerFails: true, wantCode: domainrpc.CodeUnavailable, wantError: "checkout is unavailable right now", wantCalls: 2},
		{name: "pending award", award: true, wantCode: domainrpc.CodeConflict, wantError: errAlreadyPremium.Error()},
		{name: "coverage unavailable", coverage: fakeCoverage{err: errors.New("users down")}, skipCode: true, wantError: "could not verify premium coverage"},
		{name: "checkout in progress", heldLease: true, wantCode: domainrpc.CodeConflict, wantError: "could not serialize premium coverage check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
			t.Cleanup(func() { _ = db.Close() })
			guard, coverageChecks := checkoutBasketGuard(t, db, tc)
			client, calls := checkoutProvider(t, db, 7, tc.providerFails)
			nc := testnats.Connect(t)
			require.NoError(t, SubscribeCheckout(bus.RPCWiring{NC: nc, Log: zap.NewNop()}, client, CheckoutConfig{Prefix: "checkout", Guard: guard}))
			reply := requestBasket(t, nc, transactionsrpc.BasketCreateRequest{UserID: "7", Username: " Buyer ", IPAddress: " 192.0.2.1 ", PackageType: "single"})
			if !tc.skipCode {
				require.Equal(t, tc.wantCode, reply.Code)
			}
			require.Equal(t, tc.wantError, reply.Error)
			require.Len(t, calls, tc.wantCalls)
			requireCheckoutCoverageChecks(t, coverageChecks, tc)
			if tc.wantCalls != 0 {
				created, added := <-calls, <-calls
				require.True(t, created.leaseHeld, "lease must be held while creating basket")
				require.True(t, added.leaseHeld, "lease must be held while adding package")
				require.Equal(t, "/api/accounts/store/baskets", created.path)
				require.Equal(t, map[string]any{"user_id": "7", "username": "Buyer"}, created.body["custom"])
				require.Equal(t, "192.0.2.1", created.body["ip_address"])
				require.Equal(t, map[string]any{"package_id": float64(42), "quantity": float64(1), "type": "single"}, added.body)
			}
			if tc.wantError == "" {
				require.Equal(t, "basket", reply.Ident)
				require.Equal(t, "https://checkout.example/basket", reply.CheckoutURL)
			} else {
				require.Empty(t, reply.Ident)
				require.Empty(t, reply.CheckoutURL)
			}
			requireCheckoutLeaseState(t, guard, db, tc.heldLease)
		})
	}
}

type checkoutHarness struct {
	nc      *nats.Conn
	db      *ent.Client
	calls   <-chan checkoutProviderCall
	lookups chan usersrpc.AdminRequest
}

func newCheckoutHarness(t *testing.T, leaseUser uint64, lookup func(usersrpc.AdminRequest) usersrpc.AdminReply) *checkoutHarness {
	t.Helper()
	db := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { _ = db.Close() })
	client, calls := checkoutProvider(t, db, leaseUser, false)
	nc := testnats.Connect(t)
	wiring := bus.RPCWiring{NC: nc, Log: zap.NewNop()}
	h := &checkoutHarness{nc: nc, db: db, calls: calls, lookups: make(chan usersrpc.AdminRequest, 8)}
	if lookup != nil {
		require.NoError(t, bus.Serve(wiring, "users.get", func(_ context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
			h.lookups <- req
			return lookup(req)
		}))
	}
	require.NoError(t, SubscribeCheckout(wiring, client, CheckoutConfig{Prefix: "checkout", UserGetSubject: "users.get", Guard: NewCheckoutGuard(db, fakeCoverage{})}))
	return h
}

func (h *checkoutHarness) customOf(t *testing.T, call checkoutProviderCall) map[string]any {
	t.Helper()
	custom, ok := call.body["custom"].(map[string]any)
	require.True(t, ok)
	return custom
}

func eligibleRecipient(usersrpc.AdminRequest) usersrpc.AdminReply {
	return usersrpc.AdminReply{User: &usersrpc.AdminUserView{ID: 9, Username: "Recipient", Status: "free"}}
}

func TestCheckoutRPCGiftRecipientAndAttribution(t *testing.T) {
	for _, tc := range []struct {
		name        string
		view        *usersrpc.AdminUserView
		note        string
		unavailable bool
		wantError   string
	}{
		{name: "eligible recipient", view: &usersrpc.AdminUserView{ID: 9, Username: "Recipient", Status: "free"}, note: "  Enjoy\x00\tpremium!  "},
		{name: "self gift", view: &usersrpc.AdminUserView{ID: 7, Username: "Buyer"}, wantError: "that is your own account — use Subscribe instead"},
		{name: "banned recipient", view: &usersrpc.AdminUserView{ID: 9, Banned: true}, wantError: errRecipientNotEligible.Error()},
		{name: "premium recipient", view: &usersrpc.AdminUserView{ID: 9, Status: "PAID"}, wantError: errRecipientAlreadyPremium.Error()},
		{name: "unregistered recipient", wantError: errRecipientNotRegistered.Error()},
		{name: "recipient lookup unavailable", unavailable: true, wantError: errRecipientLookup.Error()},
		{name: "obfuscated gift link", view: &usersrpc.AdminUserView{ID: 9, Username: "Recipient"}, note: "visit example[.]com", wantError: errGiftMessageLink.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lookup func(usersrpc.AdminRequest) usersrpc.AdminReply
			if !tc.unavailable {
				lookup = func(usersrpc.AdminRequest) usersrpc.AdminReply { return usersrpc.AdminReply{User: tc.view} }
			}
			h := newCheckoutHarness(t, 9, lookup)
			reply := requestBasket(t, h.nc, transactionsrpc.BasketCreateRequest{UserID: "7", Username: " Buyer ", RecipientUsername: " @ReCiPiEnT ", IPAddress: "2001:db8::1", PackageType: "subscription", GiftMessage: tc.note})
			require.Equal(t, tc.wantError, reply.Error)
			if !tc.unavailable {
				require.Len(t, h.lookups, 1)
				require.Equal(t, "recipient", (<-h.lookups).Username)
			}
			if tc.wantError != "" {
				if !tc.unavailable {
					require.Equal(t, domainrpc.CodeInvalid, reply.Code)
				}
				require.Empty(t, h.calls, "recipient refusal must not create a provider basket")
				count, err := h.db.GiveawayUserLease.Query().Count(t.Context())
				require.NoError(t, err)
				require.Zero(t, count, "recipient validation must precede acquiring a premium lease")
				return
			}
			require.Equal(t, "Recipient", reply.RecipientLogin)
			require.Equal(t, "basket", reply.Ident)
			require.Equal(t, "https://checkout.example/basket", reply.CheckoutURL)
			require.Len(t, h.calls, 2)
			created, added := <-h.calls, <-h.calls
			require.True(t, created.leaseHeld)
			require.True(t, added.leaseHeld)
			require.Equal(t, map[string]any{"user_id": "9", "username": "Recipient", "gifted_by": "7", "gifted_by_login": "Buyer", "gift_message": "Enjoy premium!"}, created.body["custom"])
			require.NotContains(t, created.body, "ip_address")
			require.Equal(t, "single", added.body["type"], "gifts must use a nonrecurring package")
			lease, err := h.db.GiveawayUserLease.Get(t.Context(), "user:9")
			require.NoError(t, err)
			require.False(t, lease.LeaseUntil.After(time.Now()), "recipient's lease must be released")
			count, err := h.db.GiveawayUserLease.Query().Count(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, count, "gift lease must belong to recipient, not buyer")
		})
	}
}

func TestCheckoutRPCGiftMessageIsSanitizedBeforeTheProvider(t *testing.T) {
	for _, tc := range []struct {
		name string
		note string
		want string
	}{
		{"trims surrounding space", "  hi there  ", "hi there"},
		{"keeps newlines", "line1\nline2", "line1\nline2"},
		{"turns tabs into spaces", "a\tb", "a b"},
		{"strips control characters", "hi\x00\x07 there", "hi there"},
		{"drops a blank note", "   ", ""},
		{"caps a note by runes", strings.Repeat("é", giftMessageMaxRunes+120), strings.Repeat("é", giftMessageMaxRunes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCheckoutHarness(t, 9, eligibleRecipient)
			reply := requestBasket(t, h.nc, transactionsrpc.BasketCreateRequest{UserID: "7", Username: "Buyer", RecipientUsername: "recipient", PackageType: "single", GiftMessage: tc.note})
			require.Empty(t, reply.Error)
			created := <-h.calls
			custom := h.customOf(t, created)
			if tc.want == "" {
				require.NotContains(t, custom, "gift_message")
				return
			}
			require.Equal(t, tc.want, custom["gift_message"])
		})
	}
}

func TestCheckoutRPCGiftMessageLinksAreRefusedAfterSanitizing(t *testing.T) {
	for _, tc := range []struct {
		note    string
		blocked bool
	}{
		{"visit example.com now", true},
		{"go to example . com", true},
		{"hey\x00example[.]com", true},
		{"ping me user (at) gmail dot com", true},
		{"thanks so much, enjoy premium!", false},
		{"see you at 3 p.m.", false},
	} {
		t.Run(tc.note, func(t *testing.T) {
			h := newCheckoutHarness(t, 9, eligibleRecipient)
			reply := requestBasket(t, h.nc, transactionsrpc.BasketCreateRequest{UserID: "7", Username: "Buyer", RecipientUsername: "recipient", PackageType: "single", GiftMessage: tc.note})
			if tc.blocked {
				require.Equal(t, errGiftMessageLink.Error(), reply.Error)
				require.Empty(t, h.calls)
				return
			}
			require.Empty(t, reply.Error)
		})
	}
}

func TestCheckoutRPCBuyerLoginIsTrimmedAndClamped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		login string
		want  string
	}{
		{"keeps a short login", "bagelfan", "bagelfan"},
		{"trims padding", "  bagelfan  ", "bagelfan"},
		{"keeps a login at the limit", strings.Repeat("a", twitchLoginMaxLen), strings.Repeat("a", twitchLoginMaxLen)},
		{"truncates a login over the limit", strings.Repeat("a", 100), strings.Repeat("a", twitchLoginMaxLen)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCheckoutHarness(t, 7, nil)
			reply := requestBasket(t, h.nc, transactionsrpc.BasketCreateRequest{UserID: "7", Username: tc.login, PackageType: "single"})
			require.Empty(t, reply.Error)
			require.Equal(t, map[string]any{"user_id": "7", "username": tc.want}, h.customOf(t, <-h.calls))
		})
	}
}

func TestCheckoutLeaseReleasePreservesNewOwner(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { _ = db.Close() })
	lease := entCheckoutLease{db: db}
	firstRelease, err := lease.AcquireUserLease(ctx, 7)
	require.NoError(t, err)
	_, err = lease.AcquireUserLease(ctx, 7)
	require.EqualError(t, err, "user checkout already in progress")
	firstRelease()
	secondRelease, err := lease.AcquireUserLease(ctx, 7)
	require.NoError(t, err)
	defer secondRelease()
	current, err := db.GiveawayUserLease.Get(ctx, "user:7")
	require.NoError(t, err)
	firstRelease()
	afterStaleRelease, err := db.GiveawayUserLease.Get(ctx, "user:7")
	require.NoError(t, err)
	require.Equal(t, current.Owner, afterStaleRelease.Owner)
	require.Equal(t, current.LeaseUntil, afterStaleRelease.LeaseUntil, "an old cleanup must not expire a newer owner's lease")
	_, err = lease.AcquireUserLease(ctx, 7)
	require.EqualError(t, err, "user checkout already in progress")
}
