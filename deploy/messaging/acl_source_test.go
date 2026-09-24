// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/internal/natsacl"
)

const accountsYAMLPath = "accounts.yaml"

// committedACL is accounts.yaml, the ACL natscreds compiles into the account
// JWTs both clusters enforce.
func committedACL(t *testing.T) *natsacl.ACL {
	t.Helper()
	acl, err := natsacl.LoadACL(accountsYAMLPath)
	if err != nil {
		t.Fatal(err)
	}
	return acl
}

type busUserBlock struct {
	subjects []string
}

func busUserBlocks(t *testing.T) map[string]busUserBlock {
	t.Helper()
	blocks := make(map[string]busUserBlock)
	for _, spec := range committedACL(t).Accounts {
		for name, role := range spec.Roles {
			if strings.HasSuffix(name, "_bus") {
				blocks[name] = busUserBlock{subjects: roleSubjects(role)}
			}
		}
	}
	return blocks
}

func roleSubjects(role natsacl.RoleSpec) []string {
	var subjects []string
	for _, perm := range []*natsacl.PermissionSpec{role.Publish, role.Subscribe} {
		if perm != nil {
			subjects = append(subjects, perm.Allow...)
			subjects = append(subjects, perm.Deny...)
		}
	}
	return subjects
}

func (b busUserBlock) jetStreamSubjects() []string {
	set := make(map[string]struct{})
	for _, subject := range b.subjects {
		if strings.HasPrefix(subject, "$JS") {
			set[subject] = struct{}{}
		}
	}
	return sortedKeys(set)
}

func (b busUserBlock) grants(subject string) bool {
	return slices.Contains(b.subjects, subject)
}

func rpcAccounts(t *testing.T) map[string]rpcAccount {
	t.Helper()
	accounts := make(map[string]rpcAccount)
	for name, spec := range committedACL(t).Accounts {
		users := rpcRoleNames(spec.Roles)
		if len(users) == 0 {
			continue
		}
		accounts[name] = rpcAccount{name: name, users: users, exports: rpcExports(spec.Exports), imports: rpcImports(spec.Imports)}
	}
	return accounts
}

func rpcRoleNames(roles map[string]natsacl.RoleSpec) []string {
	var names []string
	for name := range roles {
		if strings.HasSuffix(name, "_rpc") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func rpcExports(specs []natsacl.ExportSpec) []rpcGrant {
	grants := make([]rpcGrant, 0, len(specs))
	for _, spec := range specs {
		kind, subject := grantKind(spec.Service, spec.Stream)
		grants = append(grants, rpcGrant{kind: kind, subject: subject, accounts: spec.Accounts})
	}
	return grants
}

func rpcImports(specs []natsacl.ImportSpec) []rpcImport {
	imports := make([]rpcImport, 0, len(specs))
	for _, spec := range specs {
		kind, subject := grantKind(spec.Service, spec.Stream)
		imports = append(imports, rpcImport{rpcGrant: rpcGrant{kind: kind, subject: subject}, account: spec.From})
	}
	return imports
}

func grantKind(service, stream string) (string, string) {
	if service != "" {
		return "service", service
	}
	return "stream", stream
}

func exportSubject(exp natsacl.ExportSpec) string {
	_, subject := grantKind(exp.Service, exp.Stream)
	return subject
}
