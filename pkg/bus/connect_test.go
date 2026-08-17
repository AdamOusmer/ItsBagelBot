// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// Every bus connection must install the async error handler. Permission
// violations are reported out-of-band — the publish already returned nil — so
// without a handler nats.go discards them entirely, which is how a missing
// $JS.FC grant stopped both hot ingress lanes with no log line, no metric and
// no error anywhere.
func TestBaseOptionsInstallAsyncErrorHandler(t *testing.T) {
	var opts nats.Options
	for _, option := range baseOptions(connectionIdentity{name: "test"}) {
		if err := option(&opts); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}
	if opts.AsyncErrorCB == nil {
		t.Fatal("no asynchronous error handler; permission violations would be discarded silently")
	}

	core, logs := observer.New(zap.ErrorLevel)
	defer zap.ReplaceGlobals(zap.New(core))()

	// nats.go passes a nil subscription for a publish violation and puts the
	// subject in the error text, so the handler has to read it from there.
	opts.AsyncErrorCB(nil, nil, errors.New(
		`nats: permissions violation: Permissions Violation for Publish to "$JS.FC.TWITCH_INGRESS.worker_premium.abcd"`))
	// A subscribe violation carries a real subscription.
	opts.AsyncErrorCB(nil, &nats.Subscription{Subject: "twitch.ingress.event.premium"}, errors.New("nats: permissions violation"))

	entries := logs.All()
	if len(entries) != 2 {
		t.Fatalf("logged %d asynchronous errors, want 2", len(entries))
	}
	for i, want := range []string{"$JS.FC.TWITCH_INGRESS.worker_premium.abcd", "twitch.ingress.event.premium"} {
		if got := entries[i].ContextMap()["subject"]; got != want {
			t.Fatalf("logged subject = %v, want %q", got, want)
		}
	}
}

// The JetStream plane must dial the hub directly while the RPC plane stays
// leaf-first. In production NATS_LEAF_URL is set, so serverList prefers the leaf;
// busURL must override that with NATS_HUB_URL, or the 150k firehose pays a leaf
// forwarding hop on every message.
func TestBusURLPrefersHubWhenSet(t *testing.T) {
	t.Setenv("NATS_HUB_URL", "nats://nats:4222")
	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")

	// The override (NATS_URL) and the leaf are both ignored: JetStream is hub-only.
	if got := busURL("nats://nats-leaf:4222"); got != "nats://nats:4222" {
		t.Fatalf("busURL = %q, want hub-only nats://nats:4222", got)
	}
}

func TestBusURLFallsBackWhenNoHub(t *testing.T) {
	t.Setenv("NATS_HUB_URL", "")

	// Leaf configured, no hub: fall back to the leaf-first serverList.
	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")
	if got := busURL("ignored"); got != "nats://nats-leaf:4222" {
		t.Fatalf("busURL = %q, want leaf fallback", got)
	}

	// No split at all (local dev): honor the single endpoint the caller passes.
	t.Setenv("NATS_LEAF_URL", "")
	if got := busURL("nats://127.0.0.1:4222"); got != "nats://127.0.0.1:4222" {
		t.Fatalf("busURL = %q, want local override", got)
	}
}

func TestBusPublishURLPrefersNodeLocalOverride(t *testing.T) {
	t.Setenv("NATS_HUB_URL", "nats://nats-1.nats-headless:4222")
	t.Setenv("NATS_HUB_PUBLISH_URL", "nats://nats:4222")

	if got := busPublishURL("ignored"); got != "nats://nats:4222" {
		t.Fatalf("busPublishURL = %q, want node-local hub Service", got)
	}
}

// clientCertCache.get must not treat a missing mount as an error worth
// logging — that is the expected state before the cert/mount has rolled out
// fleet-wide, and errNoClientCert exists specifically so callers can tell the
// two apart.
func TestClientCertCacheMissingFileIsQuiet(t *testing.T) {
	dir := t.TempDir()
	var cache clientCertCache
	_, err := cache.get(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"))
	if !errors.Is(err, errNoClientCert) {
		t.Fatalf("err = %v, want errNoClientCert", err)
	}
}

// A cert file that exists but is not a valid PEM keypair is a real
// misconfiguration (a corrupt mount, wrong Secret key, ...), not the "not
// there yet" state, so it must NOT collapse to errNoClientCert —
// tlsSecureOption's eager check Fatals on this branch.
func TestClientCertCacheCorruptFileIsNotSilent(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certFile, []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}

	var cache clientCertCache
	_, err := cache.get(certFile, keyFile)
	if err == nil {
		t.Fatal("want an error for a corrupt cert file")
	}
	if errors.Is(err, errNoClientCert) {
		t.Fatal("a corrupt file must not be reported as errNoClientCert — that hides a real misconfiguration")
	}
}

// A valid keypair at the mount path must load successfully — the fleet-wide
// mount's whole purpose.
func TestClientCertCacheValidKeypairLoads(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	writeSelfSignedKeypair(t, certFile, keyFile, "gen-1")

	var cache clientCertCache
	cert, err := cache.get(certFile, keyFile)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("loaded certificate has no DER bytes")
	}
}

// The whole point of GetClientCertificate over a static tls.Config.Certificates
// value: once cert-manager rewrites the mounted file with a rotated cert (a
// new mtime, same path), the NEXT get() call must return the new cert, not
// the one cached from before — otherwise every handshake after day 75 of a
// 90-day cert keeps presenting the stale one until it hard-fails at day 90.
func TestClientCertCacheReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	firstDER := writeSelfSignedKeypair(t, certFile, keyFile, "gen-1")

	var cache clientCertCache
	first, err := cache.get(certFile, keyFile)
	if err != nil {
		t.Fatalf("get (first): %v", err)
	}
	if !bytes.Equal(first.Certificate[0], firstDER) {
		t.Fatal("first get() did not return the cert on disk")
	}

	// Simulate cert-manager's renewal: same paths, different content, and a
	// forced mtime bump so this doesn't race a filesystem with coarse mtime
	// resolution.
	secondDER := writeSelfSignedKeypair(t, certFile, keyFile, "gen-2")
	bumpMtime(t, certFile, keyFile)

	second, err := cache.get(certFile, keyFile)
	if err != nil {
		t.Fatalf("get (second): %v", err)
	}
	if bytes.Equal(second.Certificate[0], firstDER) {
		t.Fatal("get() after a file change still returned the stale cached cert")
	}
	if !bytes.Equal(second.Certificate[0], secondDER) {
		t.Fatal("get() after a file change did not return the new cert on disk")
	}
}

// A reload that fails (mount briefly corrupt mid-write, wrong permissions,
// ...) must not take an already-healthy process's next handshake down over
// it — get() keeps serving the last good cert and only logs the failure.
func TestClientCertCacheKeepsStaleCertOnReloadFailure(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	goodDER := writeSelfSignedKeypair(t, certFile, keyFile, "gen-1")

	var cache clientCertCache
	if _, err := cache.get(certFile, keyFile); err != nil {
		t.Fatalf("get (first): %v", err)
	}

	if err := os.WriteFile(certFile, []byte("corrupt mid-rotation"), 0o600); err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, certFile, keyFile)

	core, logs := observer.New(zap.ErrorLevel)
	defer zap.ReplaceGlobals(zap.New(core))()

	cert, err := cache.get(certFile, keyFile)
	if err != nil {
		t.Fatalf("get should degrade to the cached cert, not error: %v", err)
	}
	if !bytes.Equal(cert.Certificate[0], goodDER) {
		t.Fatal("get() did not keep serving the last good cert on a failed reload")
	}
	if len(logs.All()) == 0 {
		t.Fatal("a failed reload must still be logged, even though it is not fatal")
	}
}

// writeSelfSignedKeypair generates a throwaway ECDSA cert/key pair, writes it
// as PEM to the given paths, and returns the certificate's raw DER bytes so
// callers can tell two generated certs apart by content. cn distinguishes
// successive generations against the same paths in the reload tests; the
// fleet CA chain-of-trust itself is exercised by tlsSecureOption's RootCAs
// pool, not by this cache.
func writeSelfSignedKeypair(t *testing.T, certFile, keyFile, cn string) []byte {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}), 0o600); err != nil {
		t.Fatal(err)
	}
	return der
}

// bumpMtime forces both files' mtimes forward. A rewrite that lands within
// the same filesystem mtime tick as the original (common on fast test
// hardware / tmpfs) would otherwise look unchanged to clientCertCache.get,
// which keys its cache on mtime specifically to avoid re-parsing on every
// handshake.
func bumpMtime(t *testing.T, files ...string) {
	t.Helper()
	future := time.Now().Add(time.Hour)
	for _, f := range files {
		if err := os.Chtimes(f, future, future); err != nil {
			t.Fatal(err)
		}
	}
}

// RPC stays leaf-only even once NATS_HUB_URL is set. The leaf Service handles
// cross-node failover, while the hub is reserved for streams.
func TestRPCServerListStaysOnLeaf(t *testing.T) {
	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")
	t.Setenv("NATS_HUB_URL", "nats://nats:4222")

	if got := serverList("nats://nats-rpc:4222"); got != "nats://nats-leaf:4222" {
		t.Fatalf("serverList = %q, want leaf-only RPC endpoint", got)
	}
}
