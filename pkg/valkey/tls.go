// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

const (
	plainDataPort        = "6379"
	tlsDataPort          = "6380"
	plainSentinelPort    = "26379"
	tlsSentinelPort      = "26380"
	defaultTLSServerName = "valkey.valkey.svc.cluster.local"
)

func clientTLSConfig() (*tls.Config, error) {
	caPEM := os.Getenv("VALKEY_TLS_CA_PEM")
	if caPEM == "" {
		return nil, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, fmt.Errorf("valkey: VALKEY_TLS_CA_PEM contains no certificates")
	}
	serverName := os.Getenv("VALKEY_TLS_SERVER_NAME")
	if serverName == "" {
		serverName = defaultTLSServerName
	}
	config := &tls.Config{
		RootCAs:    pool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS12,
	}

	// mTLS: Valkey's tls-auth-clients yes rejects any connection (data port
	// AND Sentinel port -- both share this one config) that does not present
	// a cert chaining to the CA above. Deliberately file-based, not a PEM env
	// var like VALKEY_TLS_CA_PEM: that CA is public, this is a private key,
	// and a Secret volume mount keeps it out of `kubectl describe pod`, env
	// dumps, and any log/panic handler that serializes the process env.
	certFile := os.Getenv("VALKEY_TLS_CLIENT_CERT_FILE")
	keyFile := os.Getenv("VALKEY_TLS_CLIENT_KEY_FILE")
	if certFile == "" && keyFile == "" {
		// Deliberately permissive: unset means "server not requiring client
		// auth yet," which is the state every consumer is in until its own
		// Deployment is updated with the cert volume. This is what makes the
		// per-service mount + cert-issuance rollout a no-op ahead of the
		// tls-auth-clients flip, instead of a synchronized flag day.
		return config, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("valkey: VALKEY_TLS_CLIENT_CERT_FILE and VALKEY_TLS_CLIENT_KEY_FILE must both be set or both be empty")
	}
	// GetClientCertificate, not Certificates: the latter is read once when
	// this *tls.Config is built and then frozen, so a process that lives
	// past cert-manager's day-75 rotation keeps presenting the cert issued
	// at boot until it expires at day 90 and every handshake starts failing.
	// GetClientCertificate is invoked by crypto/tls on every handshake,
	// which gives the reloader below a chance to notice the rotated file.
	reloader, err := newClientCertReloader(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("valkey: loading client cert/key: %w", err)
	}
	config.GetClientCertificate = reloader.getClientCertificate
	return config, nil
}

// clientCertReloader caches the parsed client certificate and re-reads it
// from disk only when the cert file's mtime or size has moved since the last
// read. A stat is orders of magnitude cheaper than LoadX509KeyPair (which
// re-parses PEM and re-derives the key), and mtime+size is enough to detect
// cert-manager's renewal: the kubelet's Secret volume rotates in the new
// cert via an atomic symlink swap, which always changes both. Byte-for-byte
// content hashing would catch a same-second same-size edit that this misses,
// but that isn't a shape cert-manager rotation produces, so it isn't worth
// reading the file on every handshake to guard against it.
type clientCertReloader struct {
	certFile string
	keyFile  string

	mu      sync.RWMutex
	cert    *tls.Certificate
	modTime time.Time
	size    int64
}

func newClientCertReloader(certFile, keyFile string) (*clientCertReloader, error) {
	reloader := &clientCertReloader{certFile: certFile, keyFile: keyFile}
	if err := reloader.reload(); err != nil {
		return nil, err
	}
	return reloader, nil
}

// getClientCertificate implements tls.Config.GetClientCertificate.
func (r *clientCertReloader) getClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	if r.changed() {
		// Best effort: if the file is mid-write (the symlink swap isn't
		// atomic against a concurrent stat+read on every filesystem) or the
		// rotated pair is briefly invalid, keep serving the cached cert
		// instead of failing a live handshake over it. The next handshake
		// tries again.
		_ = r.reload()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cert == nil {
		return nil, fmt.Errorf("valkey: no client certificate loaded")
	}
	return r.cert, nil
}

func (r *clientCertReloader) changed() bool {
	info, err := os.Stat(r.certFile)
	if err != nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !info.ModTime().Equal(r.modTime) || info.Size() != r.size
}

func (r *clientCertReloader) reload() error {
	info, err := os.Stat(r.certFile)
	if err != nil {
		return err
	}
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return err
	}
	// LoadX509KeyPair leaves Leaf nil (parsed lazily per handshake by
	// default); parse it once here so it's cached alongside everything else.
	if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
		cert.Leaf = leaf
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cert = &cert
	r.modTime = info.ModTime()
	r.size = info.Size()
	return nil
}

func cloneTLSConfig(config *tls.Config) *tls.Config {
	if config == nil {
		return nil
	}
	return config.Clone()
}

func isSentinelAddress(address string) bool {
	_, port, err := net.SplitHostPort(address)
	return err == nil && (port == plainSentinelPort || port == tlsSentinelPort)
}

func secureAddress(address string, enabled bool) string {
	if !enabled {
		return address
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	switch port {
	case plainDataPort:
		port = tlsDataPort
	case plainSentinelPort:
		port = tlsSentinelPort
	}
	return net.JoinHostPort(host, port)
}

func nativeTLSDial(ctx context.Context, address string, dialer *net.Dialer, config *tls.Config) (net.Conn, error) {
	target := (nativeDialTarget{
		discovered:   address,
		nodeIP:       os.Getenv("NODE_IP"),
		localAddress: os.Getenv("VALKEY_LOCAL_ADDR"),
	}).address()
	connection, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil || config == nil {
		return connection, err
	}
	tlsConnection := tls.Client(connection, config.Clone())
	if err := tlsConnection.HandshakeContext(ctx); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return tlsConnection, nil
}

// nativeDialTarget keeps Sentinel's elected-primary semantics while avoiding a
// hostPort/Tailnet loop on whichever node currently owns the primary. Remote
// primary addresses and Sentinel connections are left untouched.
type nativeDialTarget struct {
	discovered   string
	nodeIP       string
	localAddress string
}

func (t nativeDialTarget) address() string {
	discovered := secureAddress(t.discovered, true)
	host, port, err := net.SplitHostPort(discovered)
	if err != nil || port != tlsDataPort {
		return discovered
	}
	if t.localAddress == "" || host != t.nodeIP {
		return discovered
	}
	return secureAddress(t.localAddress, true)
}
