package rpc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrialSubscriptionOwnershipScripts(t *testing.T) {
	if _, err := exec.LookPath("valkey-server"); err != nil {
		t.Skip("valkey-server unavailable")
	}
	if _, err := exec.LookPath("valkey-cli"); err != nil {
		t.Skip("valkey-cli unavailable")
	}
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
		return cli("EVAL", activateTrial, "3", "trial:owner", "trial:channel:42", "trial:owner_session", epoch, generation, id, session)
	}
	if got := activate("8", "3", "sub-1", "session-1"); got != "0" {
		t.Fatalf("stale epoch activated: %s", got)
	}
	if got := activate("9", "2", "sub-1", "session-1"); got != "0" {
		t.Fatalf("stale generation activated: %s", got)
	}
	if got := activate("9", "3", "sub-1", "stale-session"); got != "0" {
		t.Fatalf("stale session activated: %s", got)
	}
	if got := activate("9", "3", "sub-1", "session-1"); got != "1" {
		t.Fatalf("valid activation failed: %s", got)
	}
	if got := cli("HGET", "trial:channel:42", "subscription_id"); got != "sub-1" {
		t.Fatalf("owned ID: %s", got)
	}
	cli("HSET", "trial:channel:42", "state", "stopping")
	release := func(epoch, generation, id string) string {
		return cli("EVAL", releaseTrial, "2", "trial:owner", "trial:channel:42", epoch, generation, id)
	}
	if got := release("9", "3", "other-sub"); got != "0" {
		t.Fatalf("arbitrary delete admitted: %s", got)
	}
	if got := release("8", "3", "sub-1"); got != "0" {
		t.Fatalf("stale owner delete admitted: %s", got)
	}
	if got := release("9", "3", "sub-1"); got != "1" {
		t.Fatalf("owned delete failed: %s", got)
	}
	if got := cli("HGET", "trial:channel:42", "subscription_id"); got != "" {
		t.Fatalf("subscription not cleared: %s", got)
	}
}
