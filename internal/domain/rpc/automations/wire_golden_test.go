// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automationsrpc

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/codec"
)

var writeGolden = flag.Bool("automations.write-golden", false, "rewrite testdata/wire.json from wireGolden()")

var wireGoldenPath = filepath.Join("testdata", "wire.json")

type wireGoldenFile struct {
	Subject string     `json:"subject"`
	Request RunRequest `json:"request"`
	Reply   RunReply   `json:"reply"`
	Refused RunReply   `json:"refused"`
}

func wireGolden() wireGoldenFile {
	return wireGoldenFile{
		Subject: SubjectRun,
		Request: RunRequest{
			BroadcasterID: "1001",
			Name:          "welcome",
			Args:          map[string]string{"target": "viewer"},
			Source:        SourceDashboard,
			RequestID:     "req-1",
		},
		Reply:   RunReply{Accepted: true, RunID: "run-1"},
		Refused: RunReply{Refusal: rpc.Refused(rpc.CodeNotFound, "no such automation")},
	}
}

func TestWireGolden(t *testing.T) {
	out, err := codec.MarshalIndent(wireGolden(), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	out = append(out, '\n')
	if *writeGolden {
		if err := os.WriteFile(wireGoldenPath, out, 0o644); err != nil {
			t.Fatalf("write %s: %v", wireGoldenPath, err)
		}
		return
	}
	have, err := os.ReadFile(wireGoldenPath)
	if err != nil {
		t.Fatalf("read %s (regenerate with -automations.write-golden): %v", wireGoldenPath, err)
	}
	if string(have) != string(out) {
		t.Errorf("%s is stale; if the wire change was intended, rerun with -automations.write-golden.", wireGoldenPath)
	}
}
