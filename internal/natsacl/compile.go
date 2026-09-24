// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import (
	"fmt"
	"sort"

	"github.com/nats-io/jwt/v2"
)

// -1 caps mean no operator-imposed ceiling; the deployer's live limits govern.
var unlimitedJetStream = jwt.JetStreamLimits{
	MemoryStorage:        jwt.NoLimit,
	DiskStorage:          jwt.NoLimit,
	Streams:              jwt.NoLimit,
	Consumer:             jwt.NoLimit,
	MaxAckPending:        jwt.NoLimit,
	MemoryMaxStreamBytes: jwt.NoLimit,
	DiskMaxStreamBytes:   jwt.NoLimit,
}

func Compile(acl *ACL, keys *Keys) ([]*jwt.AccountClaims, error) {
	if err := validate(acl, keys); err != nil {
		return nil, err
	}
	order := orderAccounts(acl)
	claims := make([]*jwt.AccountClaims, 0, len(order))
	for _, name := range order {
		c, err := compileAccount(name, acl, keys)
		if err != nil {
			return nil, err
		}
		claims = append(claims, c)
	}
	return claims, nil
}

func compileAccount(name string, acl *ACL, keys *Keys) (*jwt.AccountClaims, error) {
	spec := acl.Accounts[name]
	claims := jwt.NewAccountClaims(keys.Accounts[name])
	claims.Name = name

	exports, err := buildExports(spec.Exports)
	if err != nil {
		return nil, fmt.Errorf("natsacl: account %q: %w", name, err)
	}
	claims.Exports = exports

	imports, err := buildImports(spec.Imports, keys)
	if err != nil {
		return nil, fmt.Errorf("natsacl: account %q: %w", name, err)
	}
	claims.Imports = imports

	if spec.JetStream != nil {
		claims.Limits.JetStreamLimits = unlimitedJetStream
		claims.ClusterTraffic = jwt.ClusterTraffic(spec.JetStream.ClusterTraffic)
	}

	mappings, err := resolveMappings(spec.Mappings)
	if err != nil {
		return nil, fmt.Errorf("natsacl: account %q: %w", name, err)
	}
	claims.Mappings = mappings

	addRoles(claims, name, spec.Roles, keys)
	return claims, nil
}

func collectGrants[S any, D any](specs []S, grant func(S) (string, grantKind, error), build func(string, grantKind, S) D) ([]D, error) {
	out := make([]D, 0, len(specs))
	for _, spec := range specs {
		subject, kind, err := grant(spec)
		if err != nil {
			return nil, err
		}
		out = append(out, build(subject, kind, spec))
	}
	return out, nil
}

func buildExports(specs []ExportSpec) (jwt.Exports, error) {
	exports, err := collectGrants(specs, ExportSpec.grant, newExport)
	if err != nil {
		return nil, err
	}
	return jwt.Exports(exports), nil
}

func newExport(subject string, kind grantKind, spec ExportSpec) *jwt.Export {
	return &jwt.Export{
		Subject:  jwt.Subject(subject),
		Type:     exportType(kind),
		TokenReq: len(spec.Accounts) > 0,
	}
}

func buildImports(specs []ImportSpec, keys *Keys) (jwt.Imports, error) {
	imports, err := collectGrants(specs, ImportSpec.grant, func(subject string, kind grantKind, spec ImportSpec) *jwt.Import {
		return &jwt.Import{
			Subject: jwt.Subject(subject),
			Account: keys.Accounts[spec.From],
			Type:    exportType(kind),
		}
	})
	if err != nil {
		return nil, err
	}
	return jwt.Imports(imports), nil
}

func exportType(kind grantKind) jwt.ExportType {
	if kind == kindStream {
		return jwt.Stream
	}
	return jwt.Service
}

func addRoles(claims *jwt.AccountClaims, account string, roles map[string]RoleSpec, keys *Keys) {
	names := make([]string, 0, len(roles))
	for role := range roles {
		names = append(names, role)
	}
	sort.Strings(names)
	for _, role := range names {
		scope := jwt.NewUserScope()
		scope.Key = keys.Roles[account][role]
		scope.Role = role
		scope.Template.Permissions = rolePermissions(roles[role])
		claims.SigningKeys.AddScopedSigner(scope)
	}
}

func rolePermissions(role RoleSpec) jwt.Permissions {
	var perms jwt.Permissions
	if role.Publish != nil {
		perms.Pub = jwt.Permission{Allow: role.Publish.Allow, Deny: role.Publish.Deny}
	}
	if role.Subscribe != nil {
		perms.Sub = jwt.Permission{Allow: role.Subscribe.Allow, Deny: role.Subscribe.Deny}
	}
	return perms
}
