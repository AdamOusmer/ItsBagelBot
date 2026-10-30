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

// ClusterPusher pushes and looks up account claims against one cluster,
// against the server set it read when constructed.
type ClusterPusher struct {
	cp      ClaimsPusher
	cluster ClusterName
	signer  nkeys.KeyPair
	servers []string
}

func (p *ClusterPusher) Cluster() ClusterName { return p.cluster }

func NewClusterPusher(ctx context.Context, cp ClaimsPusher, signer nkeys.KeyPair, cluster ClusterName) (*ClusterPusher, error) {
	servers, err := cp.Servers(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("aclpush: list %s servers: %w", cluster, err)
	}
	return &ClusterPusher{cp: cp, cluster: cluster, signer: signer, servers: servers}, nil
}

// Sync pushes every drifted account to the hub, then the leaf, stopping at the first failure.
func Sync(ctx context.Context, cp ClaimsPusher, signer nkeys.KeyPair, compiled []*jwt.AccountClaims) ([]PushResult, error) {
	var pushed []PushResult
	for _, cluster := range []ClusterName{ClusterHub, ClusterLeaf} {
		p, err := NewClusterPusher(ctx, cp, signer, cluster)
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
// is not natsacl.Equivalent; a missing live JWT counts as different. Each
// candidate is signed and decoded once first: an unsigned claims object
// carries empty-but-non-nil maps that a decoded JWT never does, and Equivalent
// treats that as a difference, so both sides must have made the same trip.
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
		if !natsacl.Equivalent(decodeLive(live), want) {
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
	return result, checkReplies(p.servers, replies)
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
