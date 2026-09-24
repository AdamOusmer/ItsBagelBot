// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsclaims

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/tlsenv"
)

const (
	sysClaimsUpdateSubj = "$SYS.REQ.CLAIMS.UPDATE"
	sysClaimsLookupSubj = "$SYS.REQ.ACCOUNT.%s.CLAIMS.LOOKUP"
	sysServerPingSubj   = "$SYS.REQ.SERVER.PING"

	connectTimeout      = 10 * time.Second
	defaultGatherWindow = time.Second
)

type Config struct {
	HubURL  string
	LeafURL string
	SysJWT  string
	SysSeed string
}

func (cfg Config) validate() error {
	required := []struct{ name, value string }{
		{"hub URL", cfg.HubURL}, {"leaf URL", cfg.LeafURL},
		{"system JWT", cfg.SysJWT}, {"system nkey seed", cfg.SysSeed},
	}
	for _, r := range required {
		if r.value == "" {
			return fmt.Errorf("natsclaims: %s is required", r.name)
		}
	}
	return nil
}

// Adapter implements ports.ClaimsPusher over direct connections to the hub
// and leaf clusters, authenticated as the system account's push user.
type Adapter struct {
	conns map[ports.ClusterName]*nats.Conn
}

func New(cfg Config) (*Adapter, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	hub, err := connect(cfg, cfg.HubURL)
	if err != nil {
		return nil, fmt.Errorf("natsclaims: connect hub: %w", err)
	}
	leaf, err := connect(cfg, cfg.LeafURL)
	if err != nil {
		hub.Close()
		return nil, fmt.Errorf("natsclaims: connect leaf: %w", err)
	}
	return &Adapter{conns: map[ports.ClusterName]*nats.Conn{ports.ClusterHub: hub, ports.ClusterLeaf: leaf}}, nil
}

func (a *Adapter) Close() {
	for _, nc := range a.conns {
		nc.Close()
	}
}

func (a *Adapter) conn(cluster ports.ClusterName) (*nats.Conn, error) {
	nc, ok := a.conns[cluster]
	if !ok {
		return nil, fmt.Errorf("natsclaims: unknown cluster %q", cluster)
	}
	return nc, nil
}

func (a *Adapter) Servers(ctx context.Context, cluster ports.ClusterName) ([]string, error) {
	nc, err := a.conn(cluster)
	if err != nil {
		return nil, err
	}
	msgs, err := gather(ctx, nc, request{subject: sysServerPingSubj}, 0)
	if err != nil {
		return nil, err
	}
	return serverNames(msgs), nil
}

func (a *Adapter) Lookup(ctx context.Context, cluster ports.ClusterName, accountKey string) (string, error) {
	nc, err := a.conn(cluster)
	if err != nil {
		return "", err
	}
	msg, err := nc.RequestWithContext(ctx, fmt.Sprintf(sysClaimsLookupSubj, accountKey), nil)
	if isTimeout(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(msg.Data), nil
}

func (a *Adapter) Push(ctx context.Context, cluster ports.ClusterName, accountJWT string, expect int) ([]ports.ClaimsReply, error) {
	nc, err := a.conn(cluster)
	if err != nil {
		return nil, err
	}
	msgs, err := gather(ctx, nc, request{subject: sysClaimsUpdateSubj, body: []byte(accountJWT)}, expect)
	if err != nil {
		return nil, err
	}
	return claimsReplies(msgs), nil
}

func connect(cfg Config, url string) (*nats.Conn, error) {
	opts := []nats.Option{nats.UserJWTAndSeed(cfg.SysJWT, cfg.SysSeed), nats.Timeout(connectTimeout), nats.Name("deployer-acl")}
	secure, err := secureOption()
	if err != nil {
		return nil, err
	}
	if secure != nil {
		opts = append(opts, secure)
	}
	return nats.Connect(url, opts...)
}

func secureOption() (nats.Option, error) {
	caPEM := env.Get("NATS_CA_PEM", "")
	if caPEM == "" {
		return nil, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, errors.New("natsclaims: invalid NATS_CA_PEM")
	}
	tlsCfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	pair, err := tlsenv.PairFromEnv("NATS_CLIENT_CERT_FILE", "NATS_CLIENT_KEY_FILE")
	if err != nil {
		return nil, err
	}
	if pair.Configured() {
		tlsCfg.GetClientCertificate = pair.GetClientCertificate
	}
	return nats.Secure(tlsCfg), nil
}

func isTimeout(err error) bool {
	return errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrNoResponders)
}

type request struct {
	subject string
	body    []byte
}

// gather collects replies to one request until expect arrive (0 means
// however many show up) or the window runs out; a short-of-expect result is
// not an error here, the caller names what is missing.
func gather(ctx context.Context, nc *nats.Conn, req request, expect int) ([]*nats.Msg, error) {
	inbox := nats.NewInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sub.Unsubscribe() }()
	if err := nc.PublishRequest(req.subject, inbox, req.body); err != nil {
		return nil, err
	}
	return collect(ctx, sub, expect), nil
}

func collect(ctx context.Context, sub *nats.Subscription, expect int) []*nats.Msg {
	var msgs []*nats.Msg
	for expect <= 0 || len(msgs) < expect {
		msg, err := sub.NextMsg(timeLeft(ctx))
		if err != nil {
			return msgs
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func timeLeft(ctx context.Context) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return defaultGatherWindow
	}
	if d := time.Until(dl); d > 0 {
		return d
	}
	return 0
}

type pingReply struct {
	Server struct {
		Name string `json:"name"`
	} `json:"server"`
}

func serverNames(msgs []*nats.Msg) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range msgs {
		name, ok := namedServer(m)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func namedServer(m *nats.Msg) (string, bool) {
	var r pingReply
	if err := codec.Unmarshal(m.Data, &r); err != nil {
		return "", false
	}
	return r.Server.Name, r.Server.Name != ""
}

type claimUpdateReply struct {
	Server *struct {
		Name string `json:"name"`
	} `json:"server"`
	Data *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"data"`
	Error *struct {
		Code        int    `json:"code"`
		Description string `json:"description"`
	} `json:"error"`
}

func claimsReplies(msgs []*nats.Msg) []ports.ClaimsReply {
	out := make([]ports.ClaimsReply, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, claimsReply(m.Data))
	}
	return out
}

func claimsReply(data []byte) ports.ClaimsReply {
	var r claimUpdateReply
	if err := codec.Unmarshal(data, &r); err != nil {
		return ports.ClaimsReply{Message: err.Error()}
	}
	reply := ports.ClaimsReply{}
	if r.Server != nil {
		reply.Server = r.Server.Name
	}
	switch {
	case r.Data != nil:
		reply.Code, reply.Message = r.Data.Code, r.Data.Message
	case r.Error != nil:
		reply.Code, reply.Message = r.Error.Code, r.Error.Description
	}
	return reply
}
