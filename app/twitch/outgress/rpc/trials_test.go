// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTrialSubscriptionOwnershipScripts(t *testing.T) {
	requireValkeyExecutables(t)
	dir, err := os.MkdirTemp("/tmp", "trialrpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "valkey.sock")
	pidfile := filepath.Join(dir, "valkey.pid")
	logfile := filepath.Join(dir, "valkey.log")
	if output, err := exec.Command("valkey-server", "--port", "0", "--unixsocket", socket, "--save", "", "--appendonly", "no", "--daemonize", "yes", "--pidfile", pidfile, "--logfile", logfile).CombinedOutput(); err != nil {
		t.Fatalf("start Valkey: %v %s", err, output)
	}
	cli := func(args ...string) string {
		t.Helper()
		command := append([]string{"-s", socket, "--raw"}, args...)
		output, err := exec.Command("valkey-cli", command...).CombinedOutput()
		if err != nil {
			t.Fatalf("valkey-cli %v: %v %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	t.Cleanup(func() {
		_ = exec.Command("valkey-cli", "-s", socket, "SHUTDOWN", "NOSAVE").Run()
		_ = os.Remove(pidfile)
	})
	for i := 0; i < 30; i++ {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(socket); err != nil {
		log, _ := os.ReadFile(logfile)
		t.Skipf("local Valkey socket unavailable in this sandbox: %s", strings.TrimSpace(string(log)))
	}

	cli("SET", "trial:owner", "9:owner")
	cli("SET", "trial:owner_session", "9:session-1")
	cli("HSET", "trial:channel:42", "generation", "3", "state", "pending")
	activate := func(epoch, generation, id, session string) string {
		return cli("EVAL", activateTrial, "3", "trial:owner", "trial:channel:42", "trial:owner_session", epoch, generation, id, session, "0")
	}
	expectTrialResult(t, activate("8", "3", "sub-1", "session-1"), "0", "stale epoch")
	expectTrialResult(t, activate("9", "2", "sub-1", "session-1"), "0", "stale generation")
	expectTrialResult(t, activate("9", "3", "sub-1", "stale-session"), "0", "stale session")
	cli("HSET", "trial:channel:42", "enabled", "0")
	expectTrialResult(t, activate("9", "3", "sub-1", "session-1"), "0", "disabled activation")
	cli("HSET", "trial:channel:42", "enabled", "1")
	cli("HSET", "trial:channel:42", "slot", "1")
	expectTrialResult(t, activate("9", "3", "sub-1", "session-1"), "0", "channel assigned to another socket")
	cli("HDEL", "trial:channel:42", "slot")
	expectTrialResult(t, activate("9", "3", "sub-1", "session-1"), "1", "valid activation")
	expectTrialResult(t, cli("HGET", "trial:channel:42", "subscription_id"), "sub-1", "owned ID")
	cli("HSET", "trial:channel:42", "state", "stopping")
	release := func(epoch, generation, id string) string {
		return cli("EVAL", releaseTrial, "2", "trial:owner", "trial:channel:42", epoch, generation, id)
	}
	expectTrialResult(t, release("9", "3", "other-sub"), "0", "arbitrary delete")
	expectTrialResult(t, release("8", "3", "sub-1"), "0", "stale owner delete")
	expectTrialResult(t, release("9", "3", "sub-1"), "1", "owned delete")
	expectTrialResult(t, cli("HGET", "trial:channel:42", "subscription_id"), "", "cleared ID")

	cli("HSET", "trial:channel:42", "state", "disabled", "enabled", "0", "subscription_id", "sub-2")
	expectTrialResult(t, release("9", "3", "sub-2"), "1", "disabled release")
	expectTrialResult(t, cli("HGET", "trial:channel:42", "state"), "disabled", "stays disabled")
	cli("HSET", "trial:channel:42", "state", "disabled", "enabled", "1", "subscription_id", "sub-3")
	expectTrialResult(t, release("9", "3", "sub-3"), "1", "rapid re-enable release")
	expectTrialResult(t, cli("HGET", "trial:channel:42", "state"), "pending", "ready to recreate")
}

func TestTrialSocketKeyContract(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{trialOwnerKey(0), "trial:owner"},
		{trialSessionKey(0), "trial:owner_session"},
		{trialOwnerKey(2), "trial:owner:2"},
		{trialSessionKey(2), "trial:owner_session:2"},
	} {
		expectTrialResult(t, tc.got, tc.want, "socket key")
	}
}

func TestTrialRequestsFailClosedWhenInvalid(t *testing.T) {
	valid := TrialSubscriptionRequest{Version: 1, BroadcasterID: "42", OwnerEpoch: 1, TrialGeneration: "1"}
	tests := []struct {
		name   string
		mutate func(*TrialSubscriptionRequest)
	}{
		{"unknown protocol version", func(r *TrialSubscriptionRequest) { r.Version = 2 }},
		{"missing broadcaster", func(r *TrialSubscriptionRequest) { r.BroadcasterID = "" }},
		{"non-positive owner epoch", func(r *TrialSubscriptionRequest) { r.OwnerEpoch = 0 }},
		{"negative slot", func(r *TrialSubscriptionRequest) { r.Slot = -1 }},
		{"slot past the last socket", func(r *TrialSubscriptionRequest) { r.Slot = 3 }},
		{"missing generation", func(r *TrialSubscriptionRequest) { r.TrialGeneration = "" }},
	}
	h := &trialSubscriptions{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid
			tt.mutate(&req)

			assert.Equal(t, TrialSubscriptionReply{Error: "stale_or_invalid_owner"}, h.create(t.Context(), req))
			assert.Equal(t, TrialSubscriptionReply{Error: "stale_or_invalid_owner"}, h.delete(t.Context(), req))
		})
	}
}

func requireValkeyExecutables(t *testing.T) {
	t.Helper()
	for _, name := range []string{"valkey-server", "valkey-cli"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s unavailable", name)
		}
	}
}

func expectTrialResult(t *testing.T, got, want, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %q, want %q", label, got, want)
	}
}
