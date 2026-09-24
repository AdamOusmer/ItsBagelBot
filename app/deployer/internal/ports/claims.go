// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ports

import "context"

type ClusterName string

const (
	ClusterHub  ClusterName = "hub"
	ClusterLeaf ClusterName = "leaf"
)

type ClaimsReply struct {
	Server  string
	Code    int
	Message string
}

// ClaimsPusher talks to a NATS operator-mode cluster's system account over
// $SYS.REQ subjects to read and push account JWTs and to count servers.
type ClaimsPusher interface {
	Servers(ctx context.Context, cluster ClusterName) ([]string, error)
	Lookup(ctx context.Context, cluster ClusterName, accountKey string) (string, error)
	Push(ctx context.Context, cluster ClusterName, accountJWT string, expect int) ([]ClaimsReply, error)
}
