// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package aclpush

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"

	"ItsBagelBot/internal/natsacl"
)

// Load reads accounts.yaml and accounts.keys.yaml and compiles them,
// exporters first, matching the order natsacl.Compile returns.
func Load(accountsFile, keysFile string) ([]*jwt.AccountClaims, error) {
	aclBytes, err := os.ReadFile(accountsFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", accountsFile, err)
	}
	keysBytes, err := os.ReadFile(keysFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", keysFile, err)
	}
	acl, err := natsacl.ParseACL(aclBytes)
	if err != nil {
		return nil, err
	}
	keys, err := natsacl.ParseKeys(keysBytes)
	if err != nil {
		return nil, err
	}
	return natsacl.Compile(acl, keys)
}

// Signer parses the operator signing key seed used to sign compiled account claims.
func Signer(seed string) (nkeys.KeyPair, error) {
	kp, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("aclpush: invalid operator signing seed: %w", err)
	}
	return kp, nil
}

// PushResult is one account's push outcome on one cluster, for stage/UI reporting.
type PushResult struct {
	Account string
	Cluster ClusterName
	Replies []ClaimsReply
}

// PushError names the servers a push did not confirm on: missing a reply,
// or replying without code 200.
type PushError struct {
	Missing []string
	Bad     []string
}

func (e *PushError) Error() string {
	var parts []string
	if len(e.Missing) > 0 {
		parts = append(parts, "no reply from "+strings.Join(e.Missing, ", "))
	}
	if len(e.Bad) > 0 {
		parts = append(parts, "non-200 reply from "+strings.Join(e.Bad, ", "))
	}
	return strings.Join(parts, "; ")
}

type confirmedKey struct {
	cluster ClusterName
	account string
}

// Syncer keeps the claim ID every server of a cluster acknowledged, per account.
// A config reload swaps in a resolver with no update callback: it rewrites the
// store CLAIMS.LOOKUP reads while running accounts keep the old claims, so a store
// token this process did not push proves nothing about what is running.
type Syncer struct {
	cp        ClaimsPusher
	signer    nkeys.KeyPair
	confirmed map[confirmedKey]string
}

func NewSyncer(cp ClaimsPusher, signer nkeys.KeyPair) *Syncer {
	return &Syncer{cp: cp, signer: signer, confirmed: map[confirmedKey]string{}}
}

// ClusterPusher pushes and looks up account claims against one cluster,
// against the server set it read when constructed.
type ClusterPusher struct {
	*Syncer
	cluster ClusterName
	servers []string
}

func (p *ClusterPusher) Cluster() ClusterName { return p.cluster }

func (s *Syncer) ClusterPusher(ctx context.Context, cluster ClusterName) (*ClusterPusher, error) {
	servers, err := s.cp.Servers(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("aclpush: list %s servers: %w", cluster, err)
	}
	return &ClusterPusher{Syncer: s, cluster: cluster, servers: servers}, nil
}

func (p *ClusterPusher) confirmedLive(live *jwt.AccountClaims) bool {
	return live != nil && p.confirmed[confirmedKey{p.cluster, live.Subject}] == live.ID
}

// Sync pushes every drifted account to the hub, then the leaf, stopping at the first failure.
func (s *Syncer) Sync(ctx context.Context, compiled []*jwt.AccountClaims) ([]PushResult, error) {
	var pushed []PushResult
	for _, cluster := range []ClusterName{ClusterHub, ClusterLeaf} {
		p, err := s.ClusterPusher(ctx, cluster)
		if err != nil {
			return pushed, err
		}
		results, err := p.pushDrifted(ctx, compiled)
		pushed = append(pushed, results...)
		if err != nil {
			return pushed, err
		}
	}
	return pushed, nil
}

func (p *ClusterPusher) pushDrifted(ctx context.Context, compiled []*jwt.AccountClaims) ([]PushResult, error) {
	drifted, err := p.Drift(ctx, compiled)
	if err != nil {
		return nil, err
	}
	var pushed []PushResult
	for _, c := range drifted {
		r, err := p.Push(ctx, c)
		if err != nil {
			return pushed, fmt.Errorf("aclpush: %s on %s: %w", c.Name, p.cluster, err)
		}
		pushed = append(pushed, r)
	}
	return pushed, nil
}

// Drift returns the subset of compiled claims whose live JWT on the cluster
// is not natsacl.Equivalent or is not the one this process last confirmed; a
// missing live JWT counts as different. Each candidate is signed and decoded
// once first: an unsigned claims object carries empty-but-non-nil maps that a
// decoded JWT never does, and Equivalent treats that as a difference, so both
// sides must have made the same trip.
func (p *ClusterPusher) Drift(ctx context.Context, compiled []*jwt.AccountClaims) ([]*jwt.AccountClaims, error) {
	var drifted []*jwt.AccountClaims
	for _, c := range compiled {
		live, err := p.cp.Lookup(ctx, p.cluster, c.Subject)
		if err != nil {
			return nil, fmt.Errorf("aclpush: lookup %s on %s: %w", c.Name, p.cluster, err)
		}
		want, err := canonicalize(c, p.signer)
		if err != nil {
			return nil, fmt.Errorf("aclpush: sign %s for comparison: %w", c.Name, err)
		}
		liveClaims := decodeLive(live)
		if !natsacl.Equivalent(liveClaims, want) || !p.confirmedLive(liveClaims) {
			drifted = append(drifted, c)
		}
	}
	return drifted, nil
}

// Push signs c with the operator signing key and pushes it, waiting for a
// reply from every server this pusher knows about.
func (p *ClusterPusher) Push(ctx context.Context, c *jwt.AccountClaims) (PushResult, error) {
	token, err := c.Encode(p.signer)
	if err != nil {
		return PushResult{}, fmt.Errorf("aclpush: sign %s: %w", c.Name, err)
	}
	replies, err := p.cp.Push(ctx, p.cluster, token, len(p.servers))
	if err != nil {
		return PushResult{}, fmt.Errorf("aclpush: push %s to %s: %w", c.Name, p.cluster, err)
	}
	result := PushResult{Account: c.Name, Cluster: p.cluster, Replies: replies}
	if err := checkReplies(p.servers, replies); err != nil {
		return result, err
	}
	p.confirmed[confirmedKey{p.cluster, c.Subject}] = c.ID
	return result, nil
}

func canonicalize(c *jwt.AccountClaims, signer nkeys.KeyPair) (*jwt.AccountClaims, error) {
	token, err := c.Encode(signer)
	if err != nil {
		return nil, err
	}
	return jwt.DecodeAccountClaims(token)
}

func decodeLive(token string) *jwt.AccountClaims {
	if token == "" {
		return nil
	}
	c, err := jwt.DecodeAccountClaims(token)
	if err != nil {
		return nil
	}
	return c
}

func checkReplies(servers []string, replies []ClaimsReply) error {
	byServer := make(map[string]ClaimsReply, len(replies))
	for _, r := range replies {
		byServer[r.Server] = r
	}
	var missing, bad []string
	for _, s := range servers {
		r, ok := byServer[s]
		switch {
		case !ok:
			missing = append(missing, s)
		case r.Code != http.StatusOK:
			bad = append(bad, s)
		}
	}
	if len(missing) == 0 && len(bad) == 0 {
		return nil
	}
	return &PushError{Missing: missing, Bad: bad}
}
