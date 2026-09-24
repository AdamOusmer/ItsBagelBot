// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"maps"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/nats-server/v2/conf"
)

var bcryptEnvAccounts = []string{
	"ADMIN_BUS", "ADMIN_RPC", "COMMANDS_BUS", "COMMANDS_RPC", "DASHBOARD_BUS", "DASHBOARD_RPC",
	"DEPLOYER_BUS", "DEPLOYER_RPC", "DISCORD_DATA_RPC", "DISCORD_ENGINE_BUS", "DISCORD_ENGINE_RPC",
	"DISCORD_INGRESS_BUS", "DISCORD_INGRESS_RPC", "DISCORD_OUTGRESS_BUS", "DISCORD_OUTGRESS_RPC",
	"GOSSIP_RPC", "LOYALTY_BUS", "LOYALTY_RPC", "MODULES_BUS", "MODULES_RPC", "NOTIFICATIONS_RPC",
	"OUTGRESS_BUS", "OUTGRESS_RPC", "PROJECTOR_BUS", "PROJECTOR_RPC", "SYS", "TRANSACTIONS_RPC",
	"TWITCH_INGRESS_BUS", "TWITCH_INGRESS_RPC", "USERS_BUS", "USERS_RPC", "WORKER_BUS", "WORKER_RPC",
}

// aclFromNatsAuthConf parses nats-auth.conf with the official nats-server
// config parser and rebuilds it as a natsacl.ACL, so the golden test and the
// parity test both compare against the same source of truth.
func aclFromNatsAuthConf(t *testing.T) *natsacl.ACL {
	t.Helper()
	for _, name := range bcryptEnvAccounts {
		t.Setenv("NATS_BCRYPT_"+name, "$2a$10$dummydummydummydummydu")
	}
	parsed, err := conf.ParseFile("nats-auth.conf")
	if err != nil {
		t.Fatal(err)
	}
	accounts, _ := parsed["accounts"].(map[string]interface{})
	acl := &natsacl.ACL{
		SystemAccount: stringField(parsed, "system_account"),
		Accounts:      make(map[string]natsacl.AccountSpec, len(accounts)),
	}
	for name, raw := range accounts {
		block, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("account %s is not a block", name)
		}
		acl.Accounts[name] = accountSpecFromConf(t, block)
	}
	return acl
}

func accountSpecFromConf(t *testing.T, block map[string]interface{}) natsacl.AccountSpec {
	t.Helper()
	return natsacl.AccountSpec{
		Exports:   exportsFromConf(t, block),
		Imports:   importsFromConf(t, block),
		JetStream: jetstreamFromConf(block),
		Mappings:  mappingsFromConf(t, block),
		Roles:     rolesFromConf(block),
	}
}

func exportsFromConf(t *testing.T, block map[string]interface{}) []natsacl.ExportSpec {
	t.Helper()
	raw, ok := block["exports"].([]interface{})
	if !ok {
		return nil
	}
	specs := make([]natsacl.ExportSpec, 0, len(raw))
	for _, item := range raw {
		entry := item.(map[string]interface{})
		spec := natsacl.ExportSpec{Accounts: stringSlice(entry["accounts"])}
		switch subject, kind := entrySubject(entry); kind {
		case "service":
			spec.Service = subject
		case "stream":
			spec.Stream = subject
		default:
			t.Fatalf("export entry has neither service nor stream: %#v", entry)
		}
		specs = append(specs, spec)
	}
	return specs
}

func entrySubject(entry map[string]interface{}) (string, string) {
	if subject, ok := entry["service"].(string); ok {
		return subject, "service"
	}
	if subject, ok := entry["stream"].(string); ok {
		return subject, "stream"
	}
	return "", ""
}

func importsFromConf(t *testing.T, block map[string]interface{}) []natsacl.ImportSpec {
	t.Helper()
	raw, ok := block["imports"].([]interface{})
	if !ok {
		return nil
	}
	specs := make([]natsacl.ImportSpec, 0, len(raw))
	for _, item := range raw {
		entry := item.(map[string]interface{})
		kind, detail := importDetail(t, entry)
		from, _ := detail["account"].(string)
		subject, _ := detail["subject"].(string)
		spec := natsacl.ImportSpec{From: from}
		if kind == "stream" {
			spec.Stream = subject
		} else {
			spec.Service = subject
		}
		specs = append(specs, spec)
	}
	return specs
}

func importDetail(t *testing.T, entry map[string]interface{}) (string, map[string]interface{}) {
	t.Helper()
	if detail, ok := entry["service"].(map[string]interface{}); ok {
		return "service", detail
	}
	if detail, ok := entry["stream"].(map[string]interface{}); ok {
		return "stream", detail
	}
	t.Fatalf("import entry has neither service nor stream: %#v", entry)
	return "", nil
}

func jetstreamFromConf(block map[string]interface{}) *natsacl.JetStreamSpec {
	js, ok := block["jetstream"].(map[string]interface{})
	if !ok {
		return nil
	}
	traffic, _ := js["cluster_traffic"].(string)
	return &natsacl.JetStreamSpec{ClusterTraffic: traffic}
}

func mappingsFromConf(t *testing.T, block map[string]interface{}) string {
	t.Helper()
	raw, ok := block["mappings"].(map[string]interface{})
	if !ok {
		return ""
	}
	got := make(map[string]string, len(raw))
	for k, v := range raw {
		got[k] = v.(string)
	}
	if want := domainMappings("hub"); !maps.Equal(got, want) {
		t.Fatalf("mappings do not match the hub-domain preset:\ngot  %v\nwant %v", got, want)
	}
	return "hub-domain"
}

func rolesFromConf(block map[string]interface{}) map[string]natsacl.RoleSpec {
	raw, ok := block["users"].([]interface{})
	if !ok {
		return nil
	}
	roles := make(map[string]natsacl.RoleSpec, len(raw))
	for _, item := range raw {
		user := item.(map[string]interface{})
		roles[user["user"].(string)] = roleSpecFromConf(user)
	}
	return roles
}

func roleSpecFromConf(user map[string]interface{}) natsacl.RoleSpec {
	perms, ok := user["permissions"].(map[string]interface{})
	if !ok {
		return natsacl.RoleSpec{}
	}
	return natsacl.RoleSpec{
		Publish:   permissionSpecFromConf(perms["publish"]),
		Subscribe: permissionSpecFromConf(perms["subscribe"]),
	}
}

func permissionSpecFromConf(raw interface{}) *natsacl.PermissionSpec {
	perm, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	return &natsacl.PermissionSpec{
		Allow: stringSlice(perm["allow"]),
		Deny:  stringSlice(perm["deny"]),
	}
}

func stringSlice(raw interface{}) []string {
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.(string))
	}
	return out
}

func stringField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
