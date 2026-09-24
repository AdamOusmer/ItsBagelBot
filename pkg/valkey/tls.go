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

	"ItsBagelBot/pkg/tlsenv"
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

	pair, err := tlsenv.PairFromEnv("VALKEY_TLS_CLIENT_CERT_FILE", "VALKEY_TLS_CLIENT_KEY_FILE")
	if err != nil {
		return nil, err
	}
	if !pair.Configured() {
		return config, nil
	}
	reloader, err := newClientCertReloader(pair.CertFile(), pair.KeyFile())
	if err != nil {
		return nil, fmt.Errorf("valkey: loading client cert/key: %w", err)
	}
	// Not Certificates: a frozen cert fails every handshake once cert-manager rotates it.
	config.GetClientCertificate = reloader.getClientCertificate
	return config, nil
}

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

func (r *clientCertReloader) getClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	if r.changed() {
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
