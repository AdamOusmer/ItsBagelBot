// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import "fmt"

type ACL struct {
	SystemAccount string                 `yaml:"system_account"`
	Accounts      map[string]AccountSpec `yaml:"accounts"`
}

type AccountSpec struct {
	Exports   []ExportSpec        `yaml:"exports,omitempty"`
	Imports   []ImportSpec        `yaml:"imports,omitempty"`
	JetStream *JetStreamSpec      `yaml:"jetstream,omitempty"`
	Mappings  string              `yaml:"mappings,omitempty"`
	Roles     map[string]RoleSpec `yaml:"roles,omitempty"`
}

type ExportSpec struct {
	Service  string   `yaml:"service,omitempty"`
	Stream   string   `yaml:"stream,omitempty"`
	Accounts []string `yaml:"accounts,omitempty"`
}

type ImportSpec struct {
	Service string `yaml:"service,omitempty"`
	Stream  string `yaml:"stream,omitempty"`
	From    string `yaml:"from"`
}

type JetStreamSpec struct {
	ClusterTraffic string `yaml:"cluster_traffic,omitempty"`
}

type RoleSpec struct {
	Publish   *PermissionSpec `yaml:"publish,omitempty"`
	Subscribe *PermissionSpec `yaml:"subscribe,omitempty"`
}

type PermissionSpec struct {
	Allow []string `yaml:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty"`
}

type Keys struct {
	Operator    string                       `yaml:"operator"`
	Accounts    map[string]string            `yaml:"accounts"`
	Roles       map[string]map[string]string `yaml:"roles"`
	Activations map[string][]Activation      `yaml:"activations,omitempty"`
}

// Activation is a token an importer holds for one token-required export.
type Activation struct {
	From    string `yaml:"from"`
	Subject string `yaml:"subject"`
	Token   string `yaml:"token"`
}

type grantKind int

const (
	kindService grantKind = iota
	kindStream
)

func (k grantKind) String() string {
	if k == kindStream {
		return "stream"
	}
	return "service"
}

func (e ExportSpec) grant() (string, grantKind, error) {
	return resolveGrant(e.Service, e.Stream)
}

func (i ImportSpec) grant() (string, grantKind, error) {
	return resolveGrant(i.Service, i.Stream)
}

func resolveGrant(service, stream string) (string, grantKind, error) {
	switch {
	case service != "" && stream == "":
		return service, kindService, nil
	case stream != "" && service == "":
		return stream, kindStream, nil
	case service != "" && stream != "":
		return "", 0, fmt.Errorf("natsacl: grant has both service %q and stream %q set", service, stream)
	default:
		return "", 0, fmt.Errorf("natsacl: grant has neither service nor stream set")
	}
}
