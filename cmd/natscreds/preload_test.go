// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestGeneratedFilesBootAnOperatorModeServer(t *testing.T) {
	cfg := testConfig(t)
	store := newFakeDoppler()
	var out bytes.Buffer
	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}
	url := startOperatorServer(t, cfg)

	t.Run("BusRoleUserPublishesAndSubscribes", func(t *testing.T) {
		nc := connectCreds(t, url, store.secrets["users"]["NATS_JWT"], store.secrets["users"]["NATS_NKEY_SEED"])
		sub, err := nc.SubscribeSync("data.users.probe")
		if err != nil {
			t.Fatal(err)
		}
		if err := nc.Publish("data.users.probe", []byte("ok")); err != nil {
			t.Fatal(err)
		}
		if _, err := sub.NextMsg(2 * time.Second); err != nil {
			t.Fatalf("users_bus did not receive its own subject: %v", err)
		}
	})

	t.Run("DeployerSysUserLooksUpPreloadedClaims", func(t *testing.T) {
		nc := connectCreds(t, url, store.secrets[deployerProject][deploySysJWTKey], store.secrets[deployerProject][deploySysSeedKey])
		keys, err := natsacl.LoadKeys(cfg.KeysPath)
		if err != nil {
			t.Fatal(err)
		}
		subject := fmt.Sprintf("$SYS.REQ.ACCOUNT.%s.CLAIMS.LOOKUP", keys.Accounts["BUS"])
		msg, err := nc.Request(subject, nil, 2*time.Second)
		if err != nil || len(msg.Data) == 0 {
			t.Fatalf("lookup BUS claims: err=%v len=%d", err, len(msg.Data))
		}
	})
}

func TestPreloadRewritesOnlyWhenAnAccountChanges(t *testing.T) {
	acl := smallTestACL()
	acl.SystemAccount = "SYS"
	mat := testMaterialFor(t, acl)
	keys, err := buildKeys(mat, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nats-accounts.conf")
	if err := writePreload(path, acl, keys, mat.operatorSigning); err != nil {
		t.Fatal(err)
	}
	first := mustReadFile(t, path)

	if err := writePreload(path, acl, keys, mat.operatorSigning); err != nil {
		t.Fatal(err)
	}
	if mustReadFile(t, path) != first {
		t.Fatal("unchanged accounts re-signed the preload")
	}

	bus := acl.Accounts["BUS"]
	bus.Exports = []natsacl.ExportSpec{{Service: "bagel.rpc.probe"}}
	acl.Accounts["BUS"] = bus
	if err := writePreload(path, acl, keys, mat.operatorSigning); err != nil {
		t.Fatal(err)
	}
	loaded, err := readPreload(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded[keys.Accounts["BUS"]].Exports; len(got) != 1 {
		t.Fatalf("BUS exports after change = %v, want the new export", got)
	}
}

func startOperatorServer(t *testing.T, cfg Config) string {
	t.Helper()
	dir := filepath.Dir(cfg.PreloadPath)
	conf := fmt.Sprintf("listen: \"127.0.0.1:-1\"\noperator: %q\nresolver: {type: full, dir: %q}\ninclude %q\n",
		cfg.OperatorJWTPath, filepath.Join(dir, "resolver"), filepath.Base(cfg.PreloadPath))
	confPath := filepath.Join(dir, "server.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := server.ProcessConfigFile(confPath)
	if err != nil {
		t.Fatalf("parse operator-mode config: %v", err)
	}
	opts.NoLog, opts.NoSigs = true, true
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("start operator-mode server: %v", err)
	}
	go s.Start()
	t.Cleanup(s.Shutdown)
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("operator-mode server not ready")
	}
	return s.ClientURL()
}

func connectCreds(t *testing.T, url, userJWT, seed string) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(url, nats.UserJWTAndSeed(userJWT, seed), nats.Timeout(2*time.Second))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}
