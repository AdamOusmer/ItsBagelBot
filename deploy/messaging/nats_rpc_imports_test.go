// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var rpcSubjectDefaultPattern = regexp.MustCompile(`env\.Get\("[A-Z0-9_]+",\s*"(bagel\.rpc\.[^"]+)"\)`)

type rpcGrant struct {
	kind     string
	subject  string
	accounts []string
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
	source  string
}

var rpcRequests = map[string][]rpcRequest{
	"projector_rpc": {
		{"bagel.rpc.internal.projection.users.get", "internal/projection/hydration"},
		{"bagel.rpc.internal.projection.modules.get", "internal/projection/hydration"},
		{"bagel.rpc.internal.projection.commands.get", "internal/projection/hydration"},
		{"bagel.rpc.loyalty.counter.get", "app/projector/loyalty.go"},
		{"bagel.rpc.loyalty.counter.board", "app/projector/loyalty.go"},
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
		{"bagel.rpc.internal.users.giveaway.pool", "app/db/transactions/giveaway"},
		{"bagel.rpc.internal.users.giveaway.coverage", "app/db/transactions/giveaway"},
		{"bagel.rpc.internal.users.giveaway.prepare", "app/db/transactions/giveaway"},
		{"bagel.rpc.internal.users.giveaway.commit", "app/db/transactions/giveaway"},
		{"bagel.rpc.internal.users.giveaway.cancel", "app/db/transactions/giveaway"},
		{"bagel.rpc.admin.user.auth.check", "app/db/transactions/giveaway"},
		{"bagel.rpc.internal.users.get", "app/db/transactions/main.go userGetSubject"},
		{"bagel.rpc.internal.billing.apply", "app/db/transactions/main.go billingSubject"},
		{"bagel.rpc.internal.users.email.get", "app/db/transactions/main.go emailSubject"},
		{"bagel.rpc.admin.notifications.send", "app/db/transactions/main.go sendSubject"},
	},
	"notifications_rpc": {
		{"bagel.rpc.internal.users.get", "app/db/notifications/main.go userGetSubject"},
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
		{"bagel.rpc.internal.users.get", "app/twitch/ingress/lib/ingress/trial_rpc.ex"},
		{"bagel.rpc.outgress.trial_subscription.create", "app/twitch/ingress/lib/ingress/trial_receiver.ex"},
		{"bagel.rpc.outgress.trial_subscription.delete", "app/twitch/ingress/lib/ingress/trial_receiver.ex"},
	},
	"dashboard_rpc": {
		{"bagel.rpc.transactions.giveaways.mine", "web/dashboard/src/lib/server/giveaways.ts"},
		{"bagel.rpc.dashboard.state_get", "web/dashboard SUB.dashboard"},
		{"bagel.rpc.dashboard.grant_save", "web/dashboard SUB.dashboard"},
		{"bagel.rpc.admin.user.audit.append", "web/dashboard SUB.audit"},
		{"bagel.rpc.delegation.list", "web/dashboard SUB.delegation"},
		{"bagel.rpc.commands.upsert", "web/dashboard SUB.commands"},
		{"bagel.rpc.commands.fetch_set_key", "web/dashboard SUB.commands"},
		{"bagel.rpc.modules.patch", "web/dashboard SUB.modules"},
		{"bagel.rpc.modules.govee.set", "web/dashboard SUB.goveeKey"},
		{"bagel.rpc.modules.spotify.app.set", "web/dashboard SUB.spotifyKey"},
		{"bagel.rpc.broadcaster.status.get", "web/dashboard SUB.broadcaster"},
		{"bagel.rpc.broadcaster.stream_info.get", "web/dashboard/src/lib/server/stream.ts"},
		{"bagel.rpc.projector.dashboard.commands.get", "web/dashboard/src/lib/server/commands-store.ts"},
		{"bagel.rpc.projector.dashboard.commands.replace", "web/dashboard/src/lib/server/commands-store.ts"},
		{"bagel.rpc.outgress.channel.get", "web/dashboard SUB.outgressRpc"},
		{"bagel.rpc.outgress.channelpoints.list", "web/dashboard SUB.outgressRpc"},
		{"bagel.rpc.outgress.accountage.get", "web/dashboard SUB.outgressRpc"},
		{"bagel.rpc.dingress.discord.setup", "web/dashboard SUB.dingressRpc"},
		{"bagel.rpc.loyalty.counter.get", "web/dashboard SUB.loyalty"},
		{"bagel.rpc.loyalty.counter.board", "web/dashboard SUB.loyalty"},
		{"bagel.rpc.gossip.govee.devices", "web/dashboard SUB.gossip"},
		{"bagel.rpc.gossip.spotify.exchange", "web/dashboard SUB.gossip"},
		{"bagel.rpc.gossip.custom.fetch", "web/dashboard SUB.gossip"},
		{"bagel.rpc.notifications.list", "web/dashboard SUB.notifications"},
		{"bagel.rpc.transactions.basket_create", "web/dashboard SUB.transactions"},
	},
	"admin_rpc": {
		{"bagel.rpc.admin.giveaways.create", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.preview", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.freeze", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.draw", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.get", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.list", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.retry", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.alerts", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.history", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.giveaways.capabilities", "web/admin/src/lib/server/giveaways.ts"},
		{"bagel.rpc.admin.user.test.set", "web/admin user inspector"},
		{"bagel.rpc.admin.user.get", "web/admin"},
		{"bagel.rpc.admin.user.auth.check", "web/admin"},
		{"bagel.rpc.admin.user.audit.list", "web/admin"},
		{"bagel.rpc.admin.notifications.send", "web/admin"},
		{"bagel.rpc.loyalty.counter.get", "web/admin"},
		{"bagel.rpc.outgress.channel.get", "web/admin"},
		{"twitch.ingress.admin.shards.get", "web/admin"},
		{"twitch.ingress.admin.trials.list", "web/admin/src/lib/server/services.ts"},
		{"twitch.ingress.admin.trials.add", "web/admin/src/lib/server/services.ts"},
		{"twitch.ingress.admin.trials.set_enabled", "web/admin/src/lib/server/services.ts"},
		{"twitch.ingress.admin.trials.remove", "web/admin/src/lib/server/services.ts"},
		{"bagel.rpc.health.users", "web/admin health page"},
		{"bagel.rpc.health.projector", "web/admin health page"},
		{"bagel.rpc.health.sesame", "web/admin health page"},
		{"bagel.rpc.admin.deploy.plan", "web/admin deploys page"},
		{"bagel.rpc.admin.deploy.start", "web/admin deploys page"},
		{"bagel.rpc.admin.deploy.get", "web/admin deploys page"},
		{"bagel.rpc.admin.deploy.list", "web/admin deploys page"},
		{"bagel.rpc.admin.deploy.resume", "web/admin deploys page"},
		{"bagel.rpc.admin.deploy.cancel", "web/admin deploys page"},
		{"bagel.rpc.admin.deploy.approve", "web/admin deploys page"},
	},
	"deployer_rpc": {
		{"bagel.rpc.admin.user.auth.check", "app/deployer/internal/rpc owner check"},
	},
}

var rpcServiceUsers = map[string]string{
	"app/db/users":         "users_rpc",
	"app/db/commands":      "commands_rpc",
	"app/db/modules":       "modules_rpc",
	"app/db/loyalty":       "loyalty_rpc",
	"app/db/discord":       "discord_data_rpc",
	"app/db/notifications": "notifications_rpc",
	"app/db/transactions":  "transactions_rpc",
	"app/projector":        "projector_rpc",
	"app/gossip":           "gossip_rpc",
	"app/twitch/outgress":  "outgress_rpc",
	"app/twitch/sesame":    "worker_rpc",
	"app/discord/engine":   "discord_engine_rpc",
	"app/discord/ingress":  "discord_ingress_rpc",
	"app/discord/outgress": "discord_outgress_rpc",
	"app/deployer":         "deployer_rpc",
}

var rpcServicesWithoutIdentity = map[string]string{
	"app/plain": "template service; config.Load() is called but no NATS connection is opened",
}

var rpcIntraAccountSubjects = map[string]map[string]string{
	"notifications_rpc": {
		"bagel.rpc.internal.notifications.cleanup": "the cleanup CronJob runs the service image with the service creds (app/db/notifications/cleanup.go)",
	},
}

var rpcUnusedDefaults = map[string]map[string]string{
	"discord_outgress_rpc": {
		"bagel.rpc.outgress": "OutgressRPCPrefix is loaded by app/discord/outgress/internal/config but nothing in the service requests it",
	},
}

func TestRPCRequestsAreImportedAndExported(t *testing.T) {
	catalog := loadRPCCatalog(t)
	for _, user := range sortedManifestUsers() {
		requester, ok := catalog.byUser[user]
		if !ok {
			t.Errorf("manifest names %s but accounts.yaml has no RPC account with that user", user)
			continue
		}
		for _, req := range rpcRequests[user] {
			t.Run(user+"/"+req.subject, func(t *testing.T) {
				catalog.assertRequestGranted(t, requester, req)
			})
		}
	}
}

func TestRPCSubjectDefaultsAreGranted(t *testing.T) {
	catalog := loadRPCCatalog(t)
	checked := 0
	for _, service := range goServiceDirs(t) {
		if _, skip := rpcServicesWithoutIdentity[service.name]; skip {
			continue
		}
		requester, ok := catalog.accountForService(t, service)
		if !ok {
			continue
		}
		for _, literal := range sortedKeys(rpcSubjectDefaults(t, service)) {
			checked++
			catalog.assertDefaultGranted(t, subjectDefault{service: service.name, literal: literal, account: requester})
		}
	}
	if checked < 40 {
		t.Fatalf("checked only %d subject defaults; the source scan likely stopped matching", checked)
	}
}

type rpcCatalog struct {
	accounts map[string]rpcAccount
	byUser   map[string]rpcAccount
}

type goService struct {
	name string
	dir  string
}

type subjectDefault struct {
	service string
	literal string
	account rpcAccount
}

func loadRPCCatalog(t *testing.T) rpcCatalog {
	t.Helper()
	accounts := rpcAccounts(t)
	byUser := make(map[string]rpcAccount)
	for _, account := range accounts {
		for _, user := range account.users {
			byUser[user] = account
		}
	}
	return rpcCatalog{accounts: accounts, byUser: byUser}
}

func sortedManifestUsers() []string {
	users := make([]string, 0, len(rpcRequests))
	for user := range rpcRequests {
		users = append(users, user)
	}
	sort.Strings(users)
	return users
}

func (c rpcCatalog) accountForService(t *testing.T, service goService) (rpcAccount, bool) {
	t.Helper()
	user, ok := rpcServiceUsers[service.name]
	if !ok {
		t.Errorf("%s has a main.go but no entry in rpcServiceUsers — map it to its RPC user or list it in rpcServicesWithoutIdentity", service.name)
		return rpcAccount{}, false
	}
	account, ok := c.byUser[user]
	if !ok {
		t.Errorf("%s maps to user %s, which has no RPC account in accounts.yaml", service.name, user)
		return rpcAccount{}, false
	}
	return account, true
}

func (c rpcCatalog) assertRequestGranted(t *testing.T, requester rpcAccount, req rpcRequest) {
	t.Helper()
	for _, subject := range []string{req.subject, req.subject + ".node.n1"} {
		if problem := c.crossAccountProblem(requester, subject); problem != "" {
			t.Errorf("%s requests %s (%s): %s", requester.users[0], subject, req.source, problem)
		}
	}
}

func (c rpcCatalog) crossAccountProblem(requester rpcAccount, subject string) string {
	imp, ok := requester.importCovering(subject)
	if !ok {
		return requester.name + " imports nothing covering it — the broker drops the request silently"
	}
	exporter, ok := c.accounts[imp.account]
	if !ok {
		return "import " + imp.subject + " names account " + imp.account + ", which is not in accounts.yaml"
	}
	exp, ok := exporter.exportCovering(subject)
	if !ok {
		return "import " + imp.subject + " from " + imp.account + " is dead — that account exports nothing covering it"
	}
	if !exp.importableBy(requester.name) {
		return "import " + imp.subject + " from " + imp.account + " is refused: that export is private to " + strings.Join(exp.accounts, ", ")
	}
	return ""
}

func (g rpcGrant) importableBy(account string) bool {
	return g.accounts == nil || slices.Contains(g.accounts, account)
}

func (c rpcCatalog) assertDefaultGranted(t *testing.T, d subjectDefault) {
	t.Helper()
	if why, ok := d.allowlisted(); ok {
		t.Logf("%s: %s — %s", d.service, d.literal, why)
		return
	}
	if problem := d.problem(); problem != "" {
		t.Errorf("%s loads %q: %s", d.service, d.literal, problem)
	}
}

func (d subjectDefault) allowlisted() (string, bool) {
	user := d.account.users[0]
	if why, ok := rpcIntraAccountSubjects[user][d.literal]; ok {
		return "intra-account (" + why + ")", true
	}
	if why, ok := rpcUnusedDefaults[user][d.literal]; ok {
		return "unused default (" + why + ")", true
	}
	return "", false
}

func (d subjectDefault) problem() string {
	if d.account.exportsCoverOrDescend(d.literal) {
		return ""
	}
	if _, ok := d.account.importCovering(d.literal); ok {
		return ""
	}
	if !d.account.importsDescend(d.literal) {
		return d.account.name + " neither exports nor imports anything covering it — a request or a subscription on it is unreachable across accounts"
	}
	if d.manifestPinsVerbUnderPrefix() {
		return ""
	}
	return d.account.name + " imports subjects under this prefix, but rpcRequests[\"" + d.account.users[0] + "\"] pins no verb under it — add the verbs the code appends"
}

func (d subjectDefault) manifestPinsVerbUnderPrefix() bool {
	for _, req := range rpcRequests[d.account.users[0]] {
		if strings.HasPrefix(req.subject, d.literal+".") {
			return true
		}
	}
	return false
}

func goServiceDirs(t *testing.T) []goService {
	t.Helper()
	root := filepath.Join("..", "..")
	var mainFiles []string
	for _, pattern := range []string{
		filepath.Join(root, "app", "*", "main.go"),
		filepath.Join(root, "app", "*", "*", "main.go"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		mainFiles = append(mainFiles, matches...)
	}
	if len(mainFiles) < 10 {
		t.Fatalf("found only %d app/**/main.go files; the glob no longer matches the service layout", len(mainFiles))
	}
	services := make([]goService, 0, len(mainFiles))
	for _, mainFile := range mainFiles {
		dir := filepath.Dir(mainFile)
		name, err := filepath.Rel(root, dir)
		if err != nil {
			t.Fatal(err)
		}
		services = append(services, goService{name: filepath.ToSlash(name), dir: dir})
	}
	return services
}

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
		if got := (rpcGrant{kind: "service", subject: c.pattern}).covers(c.subject); got != c.want {
			t.Errorf("(%q).covers(%q) = %v, want %v", c.pattern, c.subject, got, c.want)
		}
	}
}

func (g rpcGrant) covers(subject string) bool {
	if g.kind != "service" {
		return false
	}
	p := strings.Split(g.subject, ".")
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
		if imp.covers(subject) {
			return imp, true
		}
	}
	return rpcImport{}, false
}

func (a rpcAccount) exportCovering(subject string) (rpcGrant, bool) {
	for _, exp := range a.exports {
		if exp.covers(subject) {
			return exp, true
		}
	}
	return rpcGrant{}, false
}

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

func rpcSubjectDefaults(t *testing.T, service goService) map[string]struct{} {
	t.Helper()
	literals := make(map[string]struct{})
	err := filepath.WalkDir(service.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !isGoSource(d, path) {
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

func isGoSource(d os.DirEntry, path string) bool {
	return !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}
