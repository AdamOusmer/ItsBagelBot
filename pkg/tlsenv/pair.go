// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package tlsenv resolves the certificate/key file pair a fleet service is
// mounted with into the closures crypto/tls asks for on every handshake.
//
// Every listener and every mTLS client here is fed a cert-manager-issued cert
// from a Secret volume, under a different pair of environment variable names.
// The rules around that pair are the same everywhere and are the whole reason
// this package exists: both variables or neither (a half-set pair is a config
// error, never a silent downgrade to plaintext or to server-auth-only), and the
// pair is read from disk per handshake, not once at startup. Four packages used
// to spell those rules out themselves, with four wordings of the same error.
package tlsenv

import (
	"crypto/tls"
	"fmt"
	"os"
)

// Var is the name of an environment variable carrying a certificate or key file
// path. It is a distinct type so a path can never be passed where a variable
// name is meant: the two are both strings and the error message quotes the
// variable name back at the operator, which is useless if it holds a path.
type Var string

// Pair is a certificate/key file path pair. The zero Pair means "TLS not
// configured here", which Configured reports and which every caller reads as
// "stay plaintext" or "present no client cert" — that is what lets a cert be
// rolled out to one service at a time instead of on a flag day.
type Pair struct {
	certFile string
	keyFile  string
}

// PairFromEnv reads the paths held by certVar and keyVar.
//
// A half-set pair is an error rather than a fallback: the failure it prevents is
// a service that looks healthy until the peer starts asking for the cert (a
// verify:false NATS listener being flipped, traefik gaining a ServersTransport),
// which is the one moment nobody is watching.
func PairFromEnv(certVar, keyVar Var) (Pair, error) {
	pair := Pair{certFile: os.Getenv(string(certVar)), keyFile: os.Getenv(string(keyVar))}
	if (pair.certFile == "") != (pair.keyFile == "") {
		return Pair{}, fmt.Errorf("tlsenv: %s and %s must both be set or both empty", certVar, keyVar)
	}
	return pair, nil
}

// Configured reports whether both paths are set, i.e. whether the caller should
// install TLS at all.
func (p Pair) Configured() bool { return p.certFile != "" }

// CertFile returns the certificate path, for the few APIs that insist on taking
// the paths themselves instead of a closure.
func (p Pair) CertFile() string { return p.certFile }

// KeyFile returns the private key path. See CertFile.
func (p Pair) KeyFile() string { return p.keyFile }

// Load reads and parses the pair from disk. Callers that can fail loudly at
// boot call it once up front so an unreadable or mismatched pair kills the
// process there, rather than surfacing on the first handshake nobody is
// watching.
func (p Pair) Load() (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(p.certFile, p.keyFile)
	if err != nil {
		return nil, fmt.Errorf("tlsenv: load key pair %s: %w", p.certFile, err)
	}
	return &cert, nil
}

// GetCertificate satisfies tls.Config.GetCertificate: a server-side pair, read
// from disk on every handshake.
//
// Per-handshake rather than a Certificates snapshot because cert-manager renews
// at day 75 and the kubelet swaps the mounted files in place; a snapshot taken
// at boot keeps being presented until it expires at day 90 and every handshake
// starts failing. Handshake volume on these listeners is probes plus a reused
// traefik connection, so the re-read costs pennies and buys rotation with no
// restart and no manual step. Callers that handshake often enough for that to
// matter should cache on top (see pkg/valkey's stat-gated reloader).
func (p Pair) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return p.Load()
}

// GetClientCertificate satisfies tls.Config.GetClientCertificate: the client
// half of GetCertificate, with the same per-handshake re-read and the same
// reason for it.
func (p Pair) GetClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	return p.Load()
}
