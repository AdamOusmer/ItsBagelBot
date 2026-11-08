// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package transport

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	shared "ItsBagelBot/pkg/websocket"
)

type nativePort struct {
	proofs []string
	closed bool
}

func (n *nativePort) DialAuthorized(_ context.Context, path string, auth shared.Authorization) (Conn, shared.Reply, error) {
	n.proofs = append(n.proofs, auth.Proof)
	if auth.Proof == "initial" {
		return nil, shared.Reply{Status: 401, Nonce: "fresh-nonce"}, shared.ErrNonce
	}
	return &nativeConnection{}, shared.Reply{Status: 101}, nil
}
func (n *nativePort) PostAuthorized(_ context.Context, _ string, auth shared.Authorization, _ string, _ []byte) (shared.Reply, error) {
	n.proofs = append(n.proofs, auth.Proof)
	return shared.Reply{Status: http.StatusNoContent}, nil
}
func (n *nativePort) Close() error { n.closed = true; return nil }

type nativeConnection struct{}

func (*nativeConnection) Recv(context.Context, *protocol.Message) error { return nil }
func (*nativeConnection) Close() error                                  { return nil }

func TestProductionTransportRequiresNativePort(t *testing.T) {
	if _, err := NewClient(Config{Endpoint: "https://events.example"}); !errors.Is(err, ErrNative) {
		t.Fatal("production transport silently constructed direct networking")
	}
}

func TestNativePortReceivesFreshProofAfterNonceChallenge(t *testing.T) {
	port := &nativePort{}
	client, err := NewClient(Config{Endpoint: "https://events.example", Native: port, Proof: func(method, target, token, nonce string) (string, error) {
		if nonce == "" {
			return "initial", nil
		}
		return nonce, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Dial(t.Context(), Request{Token: "native"}); err != nil {
		t.Fatal(err)
	}
	if len(port.proofs) != 2 || port.proofs[0] != "initial" || port.proofs[1] != "fresh-nonce" {
		t.Fatal("native port did not receive separately generated challenge proof")
	}
	if err = client.Bind(t.Context(), BindRequest{SessionID: "session", Token: "native"}); err != nil {
		t.Fatal(err)
	}
	if err = client.Close(); err != nil || !port.closed {
		t.Fatal("native account lifetime did not close its port")
	}
}

func TestDirectConstructorStaysInFixtures(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "frontend" {
				return filepath.SkipDir
			}
			return nil
		}
		if !productionGoFile(root, file) {
			return nil
		}
		if directReference(file) {
			offenders = append(offenders, file)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 0 {
		t.Fatalf("production code bypasses isolated native networking: %v", offenders)
	}
}

func productionGoFile(root, file string) bool {
	return strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, "_test.go") && file != filepath.Join(root, "internal", "bridge", "transport", "direct.go")
}

func directReference(file string) bool {
	source, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		return true
	}
	found := false
	ast.Inspect(source, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && identifier.Name == "NewDirectClient" {
			found = true
			return false
		}
		return true
	})
	return found
}
