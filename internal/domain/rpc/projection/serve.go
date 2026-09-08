// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

// The guard chain these types ride -- reject an empty user id, reject one that
// is not a uint64, turn a load error into the reply's error field -- used to
// live here as ServeProjection, with the three projection services as its only
// callers. It moved to pkg/bus (Requesting, Failing, ServeForUser) once the
// count of hand-copied copies of the same prologue elsewhere in app/db reached
// sixteen: the guard is a property of every user-scoped RPC verb, not of the
// projection surface, and only a bus-level home can be reached by services
// that have no business importing a projection contract.
//
// What stays here is the request half. The reply half (Failed) now comes from
// the rpc.Refusal each projection reply embeds, which also gives them the
// machine-readable code beside the sentence.

// Requested satisfies bus.Requesting for the shared projection request.
func (r Request) Requested() string { return r.UserID }

// Failed satisfies bus.Failing for the users projection reply.

// Failed satisfies bus.Failing for the commands projection reply.

// Failed satisfies bus.Failing for the modules projection reply.
