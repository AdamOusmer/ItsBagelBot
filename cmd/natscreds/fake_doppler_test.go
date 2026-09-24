// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

// fakeDoppler is an in-memory Doppler for tests: no network, no real CLI.
type fakeDoppler struct {
	projects map[string]bool
	secrets  map[string]map[string]string
	setCalls int
}

func newFakeDoppler() *fakeDoppler {
	return &fakeDoppler{
		projects: make(map[string]bool),
		secrets:  make(map[string]map[string]string),
	}
}

func (f *fakeDoppler) EnsureProject(project dopplerProject) error {
	name := string(project)
	f.projects[name] = true
	if f.secrets[name] == nil {
		f.secrets[name] = make(map[string]string)
	}
	return nil
}

func (f *fakeDoppler) Get(ref secretRef) (string, bool, error) {
	value, ok := f.secrets[ref.project][ref.key]
	return value, ok, nil
}

func (f *fakeDoppler) Set(ref secretRef, value string) error {
	f.setCalls++
	if f.secrets[ref.project] == nil {
		f.secrets[ref.project] = make(map[string]string)
	}
	f.secrets[ref.project][ref.key] = value
	return nil
}
