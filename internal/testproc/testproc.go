// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package testproc

import (
	"io"
	"os/exec"
	"testing"
)

const supervisor = `"$0" "$@" & server=$!; trap 'kill $server 2>/dev/null; exit' TERM INT; read -r line; kill $server 2>/dev/null; wait $server`

type Process struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func Start(binary string, args ...string) (*Process, error) {
	cmd := exec.Command("/bin/sh", append([]string{"-c", supervisor, binary}, args...)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Process{cmd: cmd, stdin: stdin}, nil
}

func (p *Process) Stop() {
	_ = p.stdin.Close()
	_ = p.cmd.Wait()
}

func StartT(t testing.TB, binary string, args ...string) *Process {
	t.Helper()
	p, err := Start(binary, args...)
	if err != nil {
		t.Fatalf("start %s: %v", binary, err)
	}
	t.Cleanup(p.Stop)
	return p
}
