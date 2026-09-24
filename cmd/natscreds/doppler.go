// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

const dopplerConfig = "prd"

// dopplerProject is a Doppler project slug, e.g. "nats-operator". It is its
// own type, not a string, so EnsureProject reads as a fixed vocabulary of one
// argument rather than an interchangeable string.
type dopplerProject string

// secretRef names one Doppler secret; every project uses the same "prd"
// config, so a project/key pair is all a reference needs.
type secretRef struct {
	project string
	key     string
}

// Doppler is the seam the tool uses for all secret storage; the real
// implementation shells out to the doppler CLI, tests use a fake.
type Doppler interface {
	EnsureProject(project dopplerProject) error
	Get(ref secretRef) (string, bool, error)
	Set(ref secretRef, value string) error
}

type cliDoppler struct{}

// EnsureProject never syncs nats-operator into Kubernetes: that only happens
// by adding a DopplerSecret manifest, which this tool must never create.
func (cliDoppler) EnsureProject(project dopplerProject) error {
	if err := ensureExists(
		func() error { return runDoppler(nil, "projects", "get", string(project)) },
		func() error { return createDopplerProject(project) },
	); err != nil {
		return err
	}
	return ensureExists(
		func() error { return runDoppler(nil, "configs", "get", dopplerConfig, "-p", string(project)) },
		func() error { return createDopplerConfig(project) },
	)
}

// ensureExists runs check and, only when it fails, runs create; used for
// every doppler resource this tool gets-or-creates.
func ensureExists(check, create func() error) error {
	if err := check(); err == nil {
		return nil
	}
	return create()
}

func createDopplerProject(project dopplerProject) error {
	if err := runDoppler(nil, "projects", "create", string(project)); err != nil {
		return fmt.Errorf("natscreds: create doppler project %s: %w", project, err)
	}
	return nil
}

func createDopplerConfig(project dopplerProject) error {
	if err := runDoppler(nil, "configs", "create", dopplerConfig, "-p", string(project), "--environment", dopplerConfig); err != nil {
		return fmt.Errorf("natscreds: create doppler config %s/%s: %w", project, dopplerConfig, err)
	}
	return nil
}

func (cliDoppler) Get(ref secretRef) (string, bool, error) {
	out, err := captureDoppler("secrets", "get", ref.key, "-p", ref.project, "-c", dopplerConfig, "--plain", "--no-exit-on-missing-secret")
	if err != nil {
		return "", false, fmt.Errorf("natscreds: get %s: %w", ref, err)
	}
	value := strings.TrimRight(out, "\n")
	return value, value != "", nil
}

// Set never passes the value on the command line (it would show in ps); it
// is piped over stdin, matching doppler secrets set's documented stdin form.
func (cliDoppler) Set(ref secretRef, value string) error {
	if err := runDoppler(strings.NewReader(value), "secrets", "set", ref.key, "-p", ref.project, "-c", dopplerConfig, "--no-interactive", "--silent"); err != nil {
		return fmt.Errorf("natscreds: set %s: %w", ref, err)
	}
	return nil
}

func (ref secretRef) String() string {
	return ref.project + "/" + ref.key
}

// runDoppler discards the child's stdout and stderr unconditionally, so a
// secret value piped on stdin can never resurface through this process's own
// output even if doppler were to echo it back.
func runDoppler(stdin *strings.Reader, args ...string) error {
	cmd := exec.Command("doppler", args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	return cmd.Run()
}

// credential is a JWT/seed pair; mintRoleCredential and mintDeployIdentity
// both write one of these to a single project.
type credential struct {
	jwtKey    string
	jwtValue  string
	seedKey   string
	seedValue string
}

func setCredential(store Doppler, project string, cred credential) error {
	if err := store.Set(secretRef{project: project, key: cred.jwtKey}, cred.jwtValue); err != nil {
		return err
	}
	return store.Set(secretRef{project: project, key: cred.seedKey}, cred.seedValue)
}

func ensureCopiedSecret(store Doppler, ref secretRef, value string) error {
	_, ok, err := store.Get(ref)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return store.Set(ref, value)
}

func captureDoppler(args ...string) (string, error) {
	cmd := exec.Command("doppler", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}
