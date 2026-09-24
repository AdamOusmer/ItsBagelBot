// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tlsenv

import (
	"crypto/tls"
	"fmt"
	"os"
	"strings"
)

type Var string

type Pair struct {
	certFile string
	keyFile  string
}

func PairFromEnv(certVar, keyVar Var) (Pair, error) {
	pair := Pair{
		certFile: strings.TrimSpace(os.Getenv(string(certVar))),
		keyFile:  strings.TrimSpace(os.Getenv(string(keyVar))),
	}
	if (pair.certFile == "") != (pair.keyFile == "") {
		return Pair{}, fmt.Errorf("tlsenv: %s and %s must both be set or both empty", certVar, keyVar)
	}
	return pair, nil
}

func (p Pair) Configured() bool { return p.certFile != "" }

func (p Pair) CertFile() string { return p.certFile }

func (p Pair) KeyFile() string { return p.keyFile }

func (p Pair) Load() (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(p.certFile, p.keyFile)
	if err != nil {
		return nil, fmt.Errorf("tlsenv: load key pair %s: %w", p.certFile, err)
	}
	return &cert, nil
}

func (p Pair) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return p.Load()
}

// Serve through this, not ListenAndServeTLS(cert, key), or renewed certs never apply.
func (p Pair) ServerConfig() (*tls.Config, error) {
	if !p.Configured() {
		return nil, nil
	}
	if _, err := p.Load(); err != nil {
		return nil, err
	}

	return &tls.Config{GetCertificate: p.GetCertificate, MinVersion: tls.VersionTLS12}, nil
}

func (p Pair) GetClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	return p.Load()
}
