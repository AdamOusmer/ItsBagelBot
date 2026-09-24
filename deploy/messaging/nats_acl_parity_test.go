// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"fmt"
	"slices"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// Claims compiled from accounts.yaml must grant exactly what nats-auth.conf
// grants today; a dropped or altered grant in accounts.yaml fails this test.
func TestACLCompiledFromYAMLMatchesNatsAuthConf(t *testing.T) {
	confACL := aclFromNatsAuthConf(t)
	fileACL, err := natsacl.LoadACL(accountsYAMLPath)
	if err != nil {
		t.Fatal(err)
	}

	assertSameAccountNames(t, confACL, fileACL)

	keys := testKeysFor(t, confACL, fileACL)
	confClaims, err := natsacl.Compile(confACL, keys)
	if err != nil {
		t.Fatalf("compiling nats-auth.conf's derived ACL: %v", err)
	}
	fileClaims, err := natsacl.Compile(fileACL, keys)
	if err != nil {
		t.Fatalf("compiling %s: %v", accountsYAMLPath, err)
	}

	confByName := claimsByName(confClaims)
	fileByName := claimsByName(fileClaims)
	for name := range confACL.Accounts {
		t.Run(name, func(t *testing.T) {
			assertAccountsEquivalent(t, name, confByName[name], fileByName[name])
		})
	}
}

func assertSameAccountNames(t *testing.T, confACL, fileACL *natsacl.ACL) {
	t.Helper()
	confNames := accountNameSet(confACL)
	fileNames := accountNameSet(fileACL)
	slices.Sort(confNames)
	slices.Sort(fileNames)
	if !slices.Equal(confNames, fileNames) {
		t.Fatalf("account sets differ:\nnats-auth.conf: %v\n%s:         %v", confNames, accountsYAMLPath, fileNames)
	}
}

func accountNameSet(acl *natsacl.ACL) []string {
	names := make([]string, 0, len(acl.Accounts))
	for name := range acl.Accounts {
		names = append(names, name)
	}
	return names
}

func claimsByName(claims []*jwt.AccountClaims) map[string]*jwt.AccountClaims {
	out := make(map[string]*jwt.AccountClaims, len(claims))
	for _, c := range claims {
		out[c.Name] = c
	}
	return out
}

func assertAccountsEquivalent(t *testing.T, name string, want, got *jwt.AccountClaims) {
	t.Helper()
	if want == nil || got == nil {
		t.Fatalf("account %s: missing compiled claims (want=%v got=%v)", name, want != nil, got != nil)
	}
	if natsacl.Equivalent(want, got) {
		return
	}
	assertSameExports(t, name, want, got)
	assertSameImports(t, name, want, got)
	assertSameJetStream(t, name, want, got)
	assertSameRoles(t, name, want, got)
	t.Fatalf("account %s: claims differ from nats-auth.conf in a way not covered by the specific checks above", name)
}

func assertSameExports(t *testing.T, name string, want, got *jwt.AccountClaims) {
	t.Helper()
	if diff := diffGrants(exportKeys(want.Exports), exportKeys(got.Exports)); diff != "" {
		t.Errorf("account %s exports differ: %s", name, diff)
	}
}

func exportKeys(exports jwt.Exports) []string {
	keys := make([]string, 0, len(exports))
	for _, e := range exports {
		keys = append(keys, fmt.Sprintf("%s %s tokenReq=%v", e.Type, e.Subject, e.TokenReq))
	}
	return keys
}

func assertSameImports(t *testing.T, name string, want, got *jwt.AccountClaims) {
	t.Helper()
	if diff := diffGrants(importKeys(want.Imports), importKeys(got.Imports)); diff != "" {
		t.Errorf("account %s imports differ: %s", name, diff)
	}
}

func importKeys(imports jwt.Imports) []string {
	keys := make([]string, 0, len(imports))
	for _, i := range imports {
		keys = append(keys, fmt.Sprintf("%s %s from=%s", i.Type, i.Subject, i.Account))
	}
	return keys
}

func assertSameJetStream(t *testing.T, name string, want, got *jwt.AccountClaims) {
	t.Helper()
	if want.ClusterTraffic != got.ClusterTraffic {
		t.Errorf("account %s ClusterTraffic = %q, want %q", name, got.ClusterTraffic, want.ClusterTraffic)
	}
	if want.Limits.JetStreamLimits != got.Limits.JetStreamLimits {
		t.Errorf("account %s JetStreamLimits = %+v, want %+v", name, got.Limits.JetStreamLimits, want.Limits.JetStreamLimits)
	}
	if len(want.Mappings) != len(got.Mappings) {
		t.Errorf("account %s has %d mappings, want %d", name, len(got.Mappings), len(want.Mappings))
	}
}

func assertSameRoles(t *testing.T, name string, want, got *jwt.AccountClaims) {
	t.Helper()
	wantKeys := want.SigningKeys.Keys()
	gotKeys := got.SigningKeys.Keys()
	if len(wantKeys) != len(gotKeys) {
		t.Errorf("account %s has %d roles, want %d", name, len(gotKeys), len(wantKeys))
	}
	for _, key := range wantKeys {
		assertRolePermissionsMatch(t, rolePair{account: name, key: key}, want, got)
	}
}

type rolePair struct {
	account string
	key     string
}

func assertRolePermissionsMatch(t *testing.T, rp rolePair, want, got *jwt.AccountClaims) {
	t.Helper()
	wantScope, ok := want.SigningKeys.GetScope(rp.key)
	if !ok {
		return
	}
	gotScope, ok := got.SigningKeys.GetScope(rp.key)
	if !ok {
		t.Errorf("account %s: role for key %s is missing from %s", rp.account, rp.key, accountsYAMLPath)
		return
	}
	wantUser, _ := wantScope.(*jwt.UserScope)
	gotUser, _ := gotScope.(*jwt.UserScope)
	if wantUser == nil || gotUser == nil {
		return
	}
	if wantUser.Role != gotUser.Role {
		t.Errorf("account %s role name = %q, want %q", rp.account, gotUser.Role, wantUser.Role)
	}
	if diff := diffGrants(permissionKeys(wantUser.Template.Permissions), permissionKeys(gotUser.Template.Permissions)); diff != "" {
		t.Errorf("account %s role %s permissions differ: %s", rp.account, wantUser.Role, diff)
	}
}

func permissionKeys(p jwt.Permissions) []string {
	var keys []string
	for _, s := range p.Pub.Allow {
		keys = append(keys, "pub allow "+s)
	}
	for _, s := range p.Pub.Deny {
		keys = append(keys, "pub deny "+s)
	}
	for _, s := range p.Sub.Allow {
		keys = append(keys, "sub allow "+s)
	}
	for _, s := range p.Sub.Deny {
		keys = append(keys, "sub deny "+s)
	}
	return keys
}

func diffGrants(want, got []string) string {
	slices.Sort(want)
	slices.Sort(got)
	if slices.Equal(want, got) {
		return ""
	}
	return fmt.Sprintf("missing %v, extra %v", setDifference(want, got), setDifference(got, want))
}

func setDifference(from, without []string) []string {
	var diff []string
	for _, v := range from {
		if !slices.Contains(without, v) {
			diff = append(diff, v)
		}
	}
	return diff
}

func testKeysFor(t *testing.T, acls ...*natsacl.ACL) *natsacl.Keys {
	t.Helper()
	keys := &natsacl.Keys{
		Operator:    newTestAccountKey(t),
		Accounts:    make(map[string]string),
		Roles:       make(map[string]map[string]string),
		Activations: make(map[string][]natsacl.Activation),
	}
	for _, acl := range acls {
		for name, spec := range acl.Accounts {
			ensureAccountKey(t, keys, name)
			ensureRoleKeys(t, keys, name, spec.Roles)
		}
	}
	for _, acl := range acls {
		addActivations(keys, acl)
	}
	return keys
}

// addActivations mints a dummy activation for every token-required export's
// importer; confACL and fileACL describe the same grants, so the same
// (importer, exporter, subject) triple is deduplicated and gets one token,
// which both compiles then see identically.
func addActivations(keys *natsacl.Keys, acl *natsacl.ACL) {
	for exporter, spec := range acl.Accounts {
		for _, exp := range spec.Exports {
			if len(exp.Accounts) == 0 {
				continue
			}
			subject := exportSubject(exp)
			for _, importer := range exp.Accounts {
				addActivation(keys, importer, exporter, subject)
			}
		}
	}
}

func exportSubject(exp natsacl.ExportSpec) string {
	if exp.Service != "" {
		return exp.Service
	}
	return exp.Stream
}

func addActivation(keys *natsacl.Keys, importer, exporter, subject string) {
	for _, a := range keys.Activations[importer] {
		if a.From == exporter && a.Subject == subject {
			return
		}
	}
	token := "test-activation:" + exporter + ":" + subject
	keys.Activations[importer] = append(keys.Activations[importer], natsacl.Activation{From: exporter, Subject: subject, Token: token})
}

func ensureAccountKey(t *testing.T, keys *natsacl.Keys, name string) {
	t.Helper()
	if _, ok := keys.Accounts[name]; !ok {
		keys.Accounts[name] = newTestAccountKey(t)
	}
}

func ensureRoleKeys(t *testing.T, keys *natsacl.Keys, account string, roles map[string]natsacl.RoleSpec) {
	t.Helper()
	if keys.Roles[account] == nil {
		keys.Roles[account] = make(map[string]string)
	}
	for role := range roles {
		if _, ok := keys.Roles[account][role]; !ok {
			keys.Roles[account][role] = newTestAccountKey(t)
		}
	}
}

func newTestAccountKey(t *testing.T) string {
	t.Helper()
	kp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	return pub
}
