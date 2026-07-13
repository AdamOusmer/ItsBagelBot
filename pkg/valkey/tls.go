package valkey

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
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
	return &tls.Config{
		RootCAs:    pool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS12,
	}, nil
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
