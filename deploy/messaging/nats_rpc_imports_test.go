// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This file is the regression gate for the RPC-plane account model's one
// silent failure mode. Every service connects to the RPC plane under its own
// account (NATS_RPC_USER), and a request only crosses accounts when the
// requester's account IMPORTS the subject from the account that EXPORTS it.
// A request on a subject with no matching import is not an error on either
// side: the broker drops it, the caller sees ErrNoResponders or a timeout,
// and the feature reads as "down" or "unavailable" with nothing to debug.
// nats-auth.conf's own comments record this biting on the gossip fetch
// rehearsal, on the govee picker, and most recently on the Overview's
// per-stream counters (#809): the projector gained a loyalty counter.get call
// in #737 and no import for it, so every go-live skipped the baseline write
// and the dashboard stayed degraded for weeks.
//
// Two tests, from two directions:
//
//   - TestRPCRequestsAreImportedAndExported is a verb-level manifest: for
//     every cross-account request a service's code makes (rpcRequests below,
//     each with the file that makes it), the requester's account must import
//     the subject, the import must name the account that exports it, and
//     that account must export a pattern covering the subject. Adding an RPC
//     call means adding a line here; the line is where the reviewer sees the
//     ACL question asked.
//   - TestRPCSubjectDefaultsAreGranted is source-derived: it scans every Go
//     service's env-var subject defaults for bagel.rpc.* literals and
//     requires each to be either served (covered by the account's exports) or
//     requested (covered by its imports). It cannot know a verb appended at a
//     call site, so when a literal is a PREFIX whose imports are narrower than
//     the prefix, it requires the manifest to pin at least one verb under it.
//     This is the test that would have failed on #737 without anyone editing
//     a test: the projector's new "bagel.rpc.loyalty" default matched no
//     export and no import.
//
// The parser is line-oriented and deliberately dumb, like the other tests on
// this config: entries are one per line, comments start with '#', and the
// exports/imports arrays close on a line that is just ']'.

var rpcAccountHeaderPattern = regexp.MustCompile(`(?m)^  ([A-Z_]+): \{`)
var rpcUserPattern = regexp.MustCompile(`\{ user: "([a-z_]+_rpc)"`)
var exportEntryPattern = regexp.MustCompile(`^\s*\{ (service|stream): "([^"]+)" \}`)
var importEntryPattern = regexp.MustCompile(`^\s*\{ (service|stream): \{ account: "([A-Z_]+)",\s*subject: "([^"]+)" \} \}`)

// rpcSubjectDefaultPattern matches the env-var-with-default idiom every Go
// service uses for its subjects. The env name is unconstrained on purpose:
// gossip's PROJECTION_FETCHES_SUBJECT has no NATS_ prefix and is exactly the
// kind of subject this test exists to notice.
var rpcSubjectDefaultPattern = regexp.MustCompile(`env\.Get\("[A-Z0-9_]+",\s*"(bagel\.rpc\.[^"]+)"\)`)

type rpcGrant struct {
	kind    string // "service" or "stream"
	subject string
}

type rpcImport struct {
	rpcGrant
	account string
}

type rpcAccount struct {
	name    string
	users   []string
	exports []rpcGrant
	imports []rpcImport
}

type rpcRequest struct {
	subject string
	// source names the file that issues the request, so a failure reads as a
	// code location rather than a subject to go hunting for.
	source string
}

// rpcRequests is the manifest: every cross-account RPC request a service
// makes, keyed by the NATS user it connects as. Subjects are the FULL verb as
// sent, never a prefix. Health probes are listed too: a missing health import
// is read as "sibling down" on the status page, not as a config error.
//
// The console services (dashboard_rpc, admin_rpc) are TypeScript and outside
// TestRPCSubjectDefaultsAreGranted's Go scan, so their entries here are the
// only coverage they get — list the verbs the pages actually call.
var rpcRequests = map[string][]rpcRequest{
	"projector_rpc": {
		{"bagel.rpc.internal.projection.users.get", "internal/projection/hydration"},
		{"bagel.rpc.internal.projection.modules.get", "internal/projection/hydration"},
		{"bagel.rpc.internal.projection.commands.get", "internal/projection/hydration"},
		{"bagel.rpc.loyalty.counter.get", "app/projector/loyalty.go"},
		{"bagel.rpc.health.users", "app/projector/main.go healthSet"},
		{"bagel.rpc.health.commands", "app/projector/main.go healthSet"},
		{"bagel.rpc.health.modules", "app/projector/main.go healthSet"},
		{"bagel.rpc.health.loyalty", "app/projector/main.go healthSet"},
		{"bagel.rpc.health.notifications", "app/projector/main.go healthSet"},
	},
	"worker_rpc": {
		{"bagel.rpc.internal.projection.users.get", "app/twitch/sesame/internal/config ProjectionUsersSubject"},
		{"bagel.rpc.projector.dashboard.modules.get", "app/twitch/sesame/internal/config ProjectionModulesSubject"},
		{"bagel.rpc.projector.dashboard.commands.get", "app/twitch/sesame/internal/config ProjectionCommandsSubject"},
		{"bagel.rpc.broadcaster.live.get", "app/twitch/sesame/internal/config ProjectionLiveSubject"},
		{"bagel.rpc.commands.upsert", "app/twitch/sesame/engine/commands_rpc.go"},
		{"bagel.rpc.commands.delete", "app/twitch/sesame/engine/commands_rpc.go"},
		{"bagel.rpc.modules.quote.add", "app/twitch/sesame/engine/quotes_rpc.go"},
		{"bagel.rpc.modules.personality.feed", "app/twitch/sesame/engine/personality_rpc.go"},
		{"bagel.rpc.modules.personality.feed.board", "app/twitch/sesame/engine/personality_rpc.go"},
		{"bagel.rpc.gossip.custom.fetch", "app/twitch/sesame/engine/gossip_rpc.go"},
		{"bagel.rpc.loyalty.balance.get", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.balance.add", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.balance.set", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.balance.spend", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.balance.transfer", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.counter.get", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.counter.create", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.counter.set", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.counter.delete", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.loyalty.counter.list", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"bagel.rpc.outgress.chatters.get", "app/twitch/sesame/engine/loyalty_tick.go"},
		{"bagel.rpc.outgress.followage.get", "app/twitch/sesame followage lookup"},
		{"bagel.rpc.outgress.accountage.get", "app/twitch/sesame accountage lookup"},
		{"bagel.rpc.outgress.uptime.get", "app/twitch/sesame uptime lookup"},
		{"bagel.rpc.health.ingress", "app/twitch/sesame/main.go"},
		{"bagel.rpc.health.outgress", "app/twitch/sesame/main.go"},
	},
	"outgress_rpc": {
		{"bagel.rpc.internal.tokens.get", "app/twitch/outgress/internal/tokenstore"},
		{"bagel.rpc.internal.tokens.save", "app/twitch/outgress/internal/tokenstore"},
		{"bagel.rpc.ingress.conduit.get", "app/twitch/outgress/internal/config ConduitSubject"},
		{"bagel.rpc.dashboard.state_get", "app/twitch/outgress/internal/config UsersStateSubject"},
		{"bagel.rpc.admin.notifications.send", "app/twitch/outgress/internal/config NotifySendSubject"},
	},
	"transactions_rpc": {
		{"bagel.rpc.admin.user.get", "app/db/transactions/main.go userGetSubject"},
		{"bagel.rpc.internal.billing.apply", "app/db/transactions/main.go billingSubject"},
		{"bagel.rpc.internal.users.email.get", "app/db/transactions/main.go emailSubject"},
		{"bagel.rpc.admin.notifications.send", "app/db/transactions/main.go sendSubject"},
	},
	"notifications_rpc": {
		{"bagel.rpc.admin.user.get", "app/db/notifications/main.go userGetSubject"},
	},
	"gossip_rpc": {
		{"bagel.rpc.internal.govee.key.get", "app/gossip/internal/core/goveekeys.go"},
		{"bagel.rpc.internal.spotify.key.get", "app/gossip/internal/core/spotifykeys.go"},
		{"bagel.rpc.internal.spotify.key.rotate", "app/gossip/internal/core/spotifykeys.go"},
		{"bagel.rpc.internal.commands.fetchkey.get", "app/gossip/internal/core/fetchkeys.go"},
		{"bagel.rpc.internal.projection.commands.fetches.get", "app/gossip/main.go newFetchProjection"},
	},
	"discord_engine_rpc": {
		{"bagel.rpc.discord-outgress.channel.create", "app/discord/engine/internal/rpcclient"},
		{"bagel.rpc.discord-outgress.channel.delete", "app/discord/engine/internal/rpcclient"},
		{"bagel.rpc.discord-outgress.channel.modify", "app/discord/engine/internal/rpcclient"},
		{"bagel.rpc.discord-outgress.channel.purge", "app/discord/engine/internal/rpcclient"},
		{"bagel.rpc.discord-outgress.member.move", "app/discord/engine/internal/rpcclient"},
		{"bagel.rpc.discord-outgress.live.online", "app/discord/engine/internal/rpcclient"},
		{"bagel.rpc.discord-outgress.live.offline", "app/discord/engine/internal/rpcclient"},
		{"bagel.rpc.discord-outgress.invite.resolve", "app/discord/engine/internal/rpcclient"},
		{"bagel.rpc.outgress.streaminfo.get", "app/discord/engine/internal/streaminfo"},
		{"bagel.rpc.health.discord-ingress", "app/discord/engine/main.go"},
		{"bagel.rpc.health.discord-outgress", "app/discord/engine/main.go"},
	},
	"discord_ingress_rpc": {
		{"bagel.rpc.internal.users.counts.get", "app/discord/ingress/internal/config"},
	},
	"twitch_ingress_rpc": {
		{"bagel.rpc.broadcaster.status.get", "app/twitch/ingress broadcaster status lookup"},
	},
	"dashboard_rpc": {
		{"bagel.rpc.dashboard.state_get", "console/dashboard SUB.dashboard"},
		{"bagel.rpc.dashboard.grant_save", "console/dashboard SUB.dashboard"},
		{"bagel.rpc.admin.user.audit.append", "console/dashboard SUB.audit"},
		{"bagel.rpc.delegation.list", "console/dashboard SUB.delegation"},
		{"bagel.rpc.commands.upsert", "console/dashboard SUB.commands"},
		{"bagel.rpc.commands.fetch_set_key", "console/dashboard SUB.commands"},
		{"bagel.rpc.modules.patch", "console/dashboard SUB.modules"},
		{"bagel.rpc.modules.govee.set", "console/dashboard SUB.goveeKey"},
		{"bagel.rpc.modules.spotify.app.set", "console/dashboard SUB.spotifyKey"},
		{"bagel.rpc.broadcaster.status.get", "console/dashboard SUB.broadcaster"},
		{"bagel.rpc.broadcaster.stream_info.get", "console/dashboard/src/lib/server/stream.ts"},
		{"bagel.rpc.projector.dashboard.commands.get", "console/dashboard/src/lib/server/commands-store.ts"},
		{"bagel.rpc.projector.dashboard.commands.replace", "console/dashboard/src/lib/server/commands-store.ts"},
		{"bagel.rpc.outgress.channel.get", "console/dashboard SUB.outgressRpc"},
		{"bagel.rpc.outgress.channelpoints.list", "console/dashboard SUB.outgressRpc"},
		{"bagel.rpc.outgress.accountage.get", "console/dashboard SUB.outgressRpc"},
		{"bagel.rpc.dingress.discord.setup", "console/dashboard SUB.dingressRpc"},
		{"bagel.rpc.loyalty.counter.get", "console/dashboard SUB.loyalty"},
		{"bagel.rpc.loyalty.counter.board", "console/dashboard SUB.loyalty"},
		{"bagel.rpc.gossip.govee.devices", "console/dashboard SUB.gossip"},
		{"bagel.rpc.gossip.spotify.exchange", "console/dashboard SUB.gossip"},
		{"bagel.rpc.gossip.custom.fetch", "console/dashboard SUB.gossip"},
		{"bagel.rpc.notifications.list", "console/dashboard SUB.notifications"},
		{"bagel.rpc.transactions.basket_create", "console/dashboard SUB.transactions"},
	},
	"admin_rpc": {
		{"bagel.rpc.admin.user.get", "console/admin"},
		{"bagel.rpc.admin.user.auth.check", "console/admin"},
		{"bagel.rpc.admin.user.audit.list", "console/admin"},
		{"bagel.rpc.admin.notifications.send", "console/admin"},
		{"bagel.rpc.loyalty.counter.get", "console/admin"},
		{"bagel.rpc.outgress.channel.get", "console/admin"},
		{"twitch.ingress.admin.shards.get", "console/admin"},
		{"bagel.rpc.health.users", "console/admin health page"},
		{"bagel.rpc.health.projector", "console/admin health page"},
		{"bagel.rpc.health.sesame", "console/admin health page"},
	},
}

// rpcServiceUsers maps each Go service directory (the one holding its main.go)
// to the NATS user it connects to the RPC plane as. The credential itself is
// injected at deploy time, so the mapping lives here by convention and a new
// service must be added before its subjects are checked at all.
var rpcServiceUsers = map[string]string{
	"app/db/users":         "users_rpc",
	"app/db/commands":      "commands_rpc",
	"app/db/modules":       "modules_rpc",
	"app/db/loyalty":       "loyalty_rpc",
	"app/db/notifications": "notifications_rpc",
	"app/db/transactions":  "transactions_rpc",
	"app/projector":        "projector_rpc",
	"app/gossip":           "gossip_rpc",
	"app/twitch/outgress":  "outgress_rpc",
	"app/twitch/sesame":    "worker_rpc",
	"app/discord/engine":   "discord_engine_rpc",
	"app/discord/ingress":  "discord_ingress_rpc",
	"app/discord/outgress": "discord_outgress_rpc",
}

// rpcServicesWithoutIdentity are main.go directories that never open an RPC
// connection, so their subject defaults (if any) are not ACL questions.
var rpcServicesWithoutIdentity = map[string]string{
	"app/plain": "template service; config.Load() is called but no NATS connection is opened",
}

// rpcIntraAccountSubjects are subjects a service both serves and requests
// under the SAME account, which need neither an export nor an import. Each
// entry says why the two ends share an identity.
var rpcIntraAccountSubjects = map[string]map[string]string{
	"notifications_rpc": {
		"bagel.rpc.internal.notifications.cleanup": "the cleanup CronJob runs the service image with the service creds (app/db/notifications/cleanup.go)",
	},
}

// rpcUnusedDefaults are subject defaults a service loads into its config but
// no code path requests or serves. Listed rather than silently skipped so a
// reader knows the gap was looked at; deleting the config field removes the
// entry.
var rpcUnusedDefaults = map[string]map[string]string{
	"discord_outgress_rpc": {
		"bagel.rpc.outgress": "OutgressRPCPrefix is loaded by app/discord/outgress/internal/config but nothing in the service requests it",
	},
}

// TestRPCRequestsAreImportedAndExported checks the manifest end to end: the
// requester imports the subject, from the account the config says exports it,
// and that account really does export a covering pattern. The node-qualified
// variant is checked alongside: requestLocalFirst asks it first and only falls
// back on ErrNoResponders, so an import that covers the plain subject but not
// the .node.* half makes the answer depend on pod placement.
func TestRPCRequestsAreImportedAndExported(t *testing.T) {
	accounts := parseRPCAccounts(t, sourceFile{name: "nats-auth.conf"}.read(t))
	byUser := rpcAccountsByUser(accounts)

	users := make([]string, 0, len(rpcRequests))
	for user := range rpcRequests {
		users = append(users, user)
	}
	sort.Strings(users)

	for _, user := range users {
		account, ok := byUser[user]
		if !ok {
			t.Errorf("manifest names %s but nats-auth.conf has no RPC account with that user", user)
			continue
		}
		for _, req := range rpcRequests[user] {
			t.Run(user+"/"+req.subject, func(t *testing.T) {
				for _, subject := range []string{req.subject, req.subject + ".node.n1"} {
					assertCrossAccountRequestGranted(t, accounts, account, subject, req.source)
				}
			})
		}
	}
}

func assertCrossAccountRequestGranted(t *testing.T, accounts map[string]rpcAccount, requester rpcAccount, subject, source string) {
	t.Helper()
	imp, ok := requester.importCovering(subject)
	if !ok {
		t.Errorf("%s requests %s (%s) but %s imports nothing covering it — the broker drops the request silently",
			requester.users[0], subject, source, requester.name)
		return
	}
	exporter, ok := accounts[imp.account]
	if !ok {
		t.Errorf("%s imports %s from %s, which is not an account in nats-auth.conf", requester.name, imp.subject, imp.account)
		return
	}
	if _, ok := exporter.exportCovering(subject); !ok {
		t.Errorf("%s imports %s from %s, but %s exports nothing covering %s — the import is dead",
			requester.name, imp.subject, imp.account, imp.account, subject)
	}
}

// TestRPCSubjectDefaultsAreGranted derives requesters from source instead of
// from the manifest. Every bagel.rpc.* subject default a Go service loads must
// be granted in one direction or the other: an export covers it (this service
// answers on it) or an import covers it (this service calls it). A literal that
// is only a prefix of narrower imports is accepted only when the manifest pins
// a verb under it, so the verbs the code actually appends are on record.
func TestRPCSubjectDefaultsAreGranted(t *testing.T) {
	accounts := parseRPCAccounts(t, sourceFile{name: "nats-auth.conf"}.read(t))
	byUser := rpcAccountsByUser(accounts)

	root := filepath.Join("..", "..")
	mainFiles, err := filepath.Glob(filepath.Join(root, "app", "*", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	nested, err := filepath.Glob(filepath.Join(root, "app", "*", "*", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	mainFiles = append(mainFiles, nested...)
	if len(mainFiles) < 10 {
		t.Fatalf("found only %d app/*/main.go files; the glob no longer matches the service layout", len(mainFiles))
	}

	checked := 0
	for _, mainFile := range mainFiles {
		dir := filepath.Dir(mainFile)
		service, err := filepath.Rel(root, dir)
		if err != nil {
			t.Fatal(err)
		}
		service = filepath.ToSlash(service)
		if _, skip := rpcServicesWithoutIdentity[service]; skip {
			continue
		}
		user, ok := rpcServiceUsers[service]
		if !ok {
			t.Errorf("%s has a main.go but no entry in rpcServiceUsers — map it to its RPC user or list it in rpcServicesWithoutIdentity", service)
			continue
		}
		account, ok := byUser[user]
		if !ok {
			t.Errorf("%s maps to user %s, which has no RPC account in nats-auth.conf", service, user)
			continue
		}
		for _, literal := range sortedKeys(rpcSubjectDefaults(t, dir)) {
			checked++
			assertSubjectDefaultGranted(t, account, service, literal)
		}
	}
	if checked < 40 {
		t.Fatalf("checked only %d subject defaults; the source scan likely stopped matching", checked)
	}
}

func assertSubjectDefaultGranted(t *testing.T, account rpcAccount, service, literal string) {
	t.Helper()
	user := account.users[0]
	if why, ok := rpcIntraAccountSubjects[user][literal]; ok {
		t.Logf("%s: %s is intra-account (%s)", service, literal, why)
		return
	}
	if why, ok := rpcUnusedDefaults[user][literal]; ok {
		t.Logf("%s: %s is an unused default (%s)", service, literal, why)
		return
	}
	if account.exportsCoverOrDescend(literal) {
		return // served by this account
	}
	if _, ok := account.importCovering(literal); ok {
		return // requested as a full subject
	}
	if account.importsDescend(literal) {
		// Narrower imports live under this prefix; the manifest has to say which
		// verbs the service actually sends so the imports can be checked at
		// verb level by TestRPCRequestsAreImportedAndExported.
		for _, req := range rpcRequests[user] {
			if strings.HasPrefix(req.subject, literal+".") {
				return
			}
		}
		t.Errorf("%s loads prefix %q and %s imports subjects under it, but rpcRequests[%q] pins no verb under that prefix — add the verbs the code appends",
			service, literal, account.name, user)
		return
	}
	t.Errorf("%s loads subject default %q but %s neither exports nor imports anything covering it — a request or a subscription on it is unreachable across accounts",
		service, literal, account.name)
}

// TestSubjectMatches pins the NATS wildcard semantics the two tests above rely
// on: '*' is exactly one token, '>' is one or more trailing tokens.
func TestSubjectMatches(t *testing.T) {
	cases := []struct {
		pattern, subject string
		want             bool
	}{
		{"bagel.rpc.loyalty.counter.get", "bagel.rpc.loyalty.counter.get", true},
		{"bagel.rpc.loyalty.counter.get", "bagel.rpc.loyalty.counter.set", false},
		{"bagel.rpc.loyalty.>", "bagel.rpc.loyalty.counter.get", true},
		{"bagel.rpc.loyalty.>", "bagel.rpc.loyalty", false},
		{"bagel.rpc.loyalty.counter.>", "bagel.rpc.loyalty.balance.get", false},
		{"bagel.rpc.health.users.node.*", "bagel.rpc.health.users.node.n1", true},
		{"bagel.rpc.health.users.node.*", "bagel.rpc.health.users.node.n1.extra", false},
		{"bagel.rpc.health.users.node.*", "bagel.rpc.health.users", false},
		{"bagel.rpc.*.get", "bagel.rpc.thing.get", true},
		{"bagel.rpc.*.get", "bagel.rpc.thing.sub.get", false},
	}
	for _, c := range cases {
		if got := subjectMatches(c.pattern, c.subject); got != c.want {
			t.Errorf("subjectMatches(%q, %q) = %v, want %v", c.pattern, c.subject, got, c.want)
		}
	}
}

// subjectMatches reports whether a NATS subject pattern covers a concrete
// subject: tokens are dot-separated, '*' matches exactly one token and '>'
// matches one or more remaining tokens.
func subjectMatches(pattern, subject string) bool {
	p := strings.Split(pattern, ".")
	s := strings.Split(subject, ".")
	for i, tok := range p {
		if tok == ">" {
			return i < len(s)
		}
		if i >= len(s) {
			return false
		}
		if tok != "*" && tok != s[i] {
			return false
		}
	}
	return len(p) == len(s)
}

func (a rpcAccount) importCovering(subject string) (rpcImport, bool) {
	for _, imp := range a.imports {
		if imp.kind == "service" && subjectMatches(imp.subject, subject) {
			return imp, true
		}
	}
	return rpcImport{}, false
}

func (a rpcAccount) exportCovering(subject string) (rpcGrant, bool) {
	for _, exp := range a.exports {
		if exp.kind == "service" && subjectMatches(exp.subject, subject) {
			return exp, true
		}
	}
	return rpcGrant{}, false
}

// exportsCoverOrDescend is the "served here" test for a subject default: the
// literal is matched by an export (a full subject, or a prefix that an
// export's own wildcard covers) or is a prefix that exports live under (the
// service appends verbs to it and exports them one by one).
func (a rpcAccount) exportsCoverOrDescend(literal string) bool {
	if _, ok := a.exportCovering(literal); ok {
		return true
	}
	for _, exp := range a.exports {
		if exp.kind == "service" && strings.HasPrefix(exp.subject, literal+".") {
			return true
		}
	}
	return false
}

func (a rpcAccount) importsDescend(literal string) bool {
	for _, imp := range a.imports {
		if imp.kind == "service" && strings.HasPrefix(imp.subject, literal+".") {
			return true
		}
	}
	return false
}

func rpcAccountsByUser(accounts map[string]rpcAccount) map[string]rpcAccount {
	byUser := make(map[string]rpcAccount)
	for _, account := range accounts {
		for _, user := range account.users {
			byUser[user] = account
		}
	}
	return byUser
}

// parseRPCAccounts reads every top-level account block that carries a *_rpc
// user and collects its exports and imports. BUS and SYS carry no such user
// and fall out naturally.
func parseRPCAccounts(t *testing.T, config string) map[string]rpcAccount {
	t.Helper()
	headers := rpcAccountHeaderPattern.FindAllStringSubmatchIndex(config, -1)
	accounts := make(map[string]rpcAccount)
	for i, header := range headers {
		end := len(config)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		body := config[header[0]:end]
		account := rpcAccount{name: config[header[2]:header[3]]}
		for _, match := range rpcUserPattern.FindAllStringSubmatch(body, -1) {
			account.users = append(account.users, match[1])
		}
		if len(account.users) == 0 {
			continue
		}
		account.exports, account.imports = parseGrantArrays(body)
		accounts[account.name] = account
	}
	if len(accounts) < 10 {
		t.Fatalf("parsed only %d RPC accounts; the header pattern no longer matches nats-auth.conf", len(accounts))
	}
	return accounts
}

func parseGrantArrays(body string) (exports []rpcGrant, imports []rpcImport) {
	section := ""
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"), trimmed == "":
			continue
		case trimmed == "exports: [":
			section = "exports"
			continue
		case trimmed == "imports: [":
			section = "imports"
			continue
		case trimmed == "]":
			section = ""
			continue
		}
		switch section {
		case "exports":
			if m := exportEntryPattern.FindStringSubmatch(line); m != nil {
				exports = append(exports, rpcGrant{kind: m[1], subject: m[2]})
			}
		case "imports":
			if m := importEntryPattern.FindStringSubmatch(line); m != nil {
				imports = append(imports, rpcImport{rpcGrant: rpcGrant{kind: m[1], subject: m[3]}, account: m[2]})
			}
		}
	}
	return exports, imports
}

// rpcSubjectDefaults collects every bagel.rpc.* env-var default under a
// service directory (non-test Go files, recursively), so internal/config
// packages are covered along with main.go.
func rpcSubjectDefaults(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	literals := make(map[string]struct{})
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range rpcSubjectDefaultPattern.FindAllStringSubmatch(string(body), -1) {
			literals[m[1]] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return literals
}
