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

	"github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	rpcSubjectDefaultPattern = regexp.MustCompile(`env\.Get\("[A-Z0-9_]+",\s*"(rpc\.[^"]+)"\)`)
	confSubjectUsePattern    = regexp.MustCompile(`conf\.([A-Za-z]+)\.Get\(\)`)
	confSubjectDeclPattern   = regexp.MustCompile(`([A-Za-z]+)\s*=\s*Subject\{"[A-Z0-9_]+",\s*"(rpc\.[^"]+)"\}`)
)

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
	"users_rpc": {
		{"rpc.internal.admin.audit.append", "internal/adminauth durable audit delivery"},
		{"rpc.admin.user.auth.check", "internal/adminauth"},
		{"rpc.admin.user.audit.append", "app/db/users audit writer"},
		{"rpc.internal.admin.staff.active", "app/db/users giveaway eligibility"},
	},
	"projector_rpc": {
		{"rpc.internal.projection.users.get", "internal/projection/hydration"},
		{"rpc.internal.projection.modules.get", "internal/projection/hydration"},
		{"rpc.internal.projection.commands.get", "internal/projection/hydration"},
		{"rpc.internal.projection.modules.automations.get", "app/projector/automations.go"},
		{"rpc.loyalty.counter.get", "app/projector/loyalty.go"},
		{"rpc.loyalty.counter.board", "app/projector/loyalty.go"},
		{"rpc.health.admin", "app/projector/main.go healthSet"},
		{"rpc.health.users", "app/projector/main.go healthSet"},
		{"rpc.health.commands", "app/projector/main.go healthSet"},
		{"rpc.health.modules", "app/projector/main.go healthSet"},
		{"rpc.health.loyalty", "app/projector/main.go healthSet"},
		{"rpc.health.notifications", "app/projector/main.go healthSet"},
	},
	"worker_rpc": {
		{"rpc.internal.projection.users.get", "app/twitch/sesame/internal/config ProjectionUsersSubject"},
		{"rpc.projector.dashboard.modules.get", "app/twitch/sesame/internal/config ProjectionModulesSubject"},
		{"rpc.projector.dashboard.commands.get", "app/twitch/sesame/internal/config ProjectionCommandsSubject"},
		{"rpc.broadcaster.live.get", "app/twitch/sesame/internal/config ProjectionLiveSubject"},
		{"rpc.commands.upsert", "app/twitch/sesame/engine/commands_rpc.go"},
		{"rpc.commands.delete", "app/twitch/sesame/engine/commands_rpc.go"},
		{"rpc.modules.quote.add", "app/twitch/sesame/engine/quotes_rpc.go"},
		{"rpc.modules.variable.get", "app/twitch/sesame/engine/variables_rpc.go"},
		{"rpc.modules.variable.set", "app/twitch/sesame/engine/variables_rpc.go"},
		{"rpc.modules.variable.add", "app/twitch/sesame/engine/variables_rpc.go"},
		{"rpc.modules.variable.delete", "app/twitch/sesame/engine/variables_rpc.go"},
		{"rpc.modules.cooldown.claim", "app/twitch/sesame/engine/cooldown_long.go"},
		{"rpc.modules.cooldown.release", "app/twitch/sesame/engine/cooldown_long.go"},
		{"rpc.modules.cooldown.peek", "app/twitch/sesame/engine/cooldown_long.go"},
		{"rpc.modules.cooldown.extend", "app/twitch/sesame/engine/cooldown_long.go"},
		{"rpc.modules.countdown.start", "app/twitch/sesame/engine/countdown_rpc.go"},
		{"rpc.modules.countdown.add", "app/twitch/sesame/engine/countdown_rpc.go"},
		{"rpc.modules.countdown.peek", "app/twitch/sesame/engine/countdown_rpc.go"},
		{"rpc.modules.countdown.claim", "app/twitch/sesame/engine/countdown_rpc.go"},
		{"rpc.modules.countdown.ack", "app/twitch/sesame/engine/countdown_rpc.go"},
		{"rpc.modules.countdown.progress", "app/twitch/sesame/engine/countdown_rpc.go"},

		{"rpc.modules.personality.feed", "app/twitch/sesame/engine/personality_rpc.go"},
		{"rpc.modules.personality.feed.board", "app/twitch/sesame/engine/personality_rpc.go"},
		{"rpc.gossip.custom.fetch", "app/twitch/sesame/engine/gossip_rpc.go"},
		{"rpc.gossip.custom.post", "app/twitch/sesame/engine/automation_action_effects.go"},
		{"rpc.internal.projection.modules.automations.get", "internal/projection/client_automations.go"},
		{"rpc.loyalty.balance.get", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.balance.add", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.balance.set", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.balance.spend", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.balance.transfer", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.counter.get", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.counter.create", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.counter.set", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.counter.delete", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.loyalty.counter.list", "app/twitch/sesame/engine/loyalty_rpc.go"},
		{"rpc.outgress.chatters.get", "app/twitch/sesame/engine/loyalty_tick.go"},
		{"rpc.outgress.followage.get", "app/twitch/sesame followage lookup"},
		{"rpc.outgress.accountage.get", "app/twitch/sesame accountage lookup"},
		{"rpc.outgress.uptime.get", "app/twitch/sesame uptime lookup"},
		{"rpc.health.ingress", "app/twitch/sesame/main.go"},
		{"rpc.health.outgress", "app/twitch/sesame/main.go"},
	},
	"outgress_rpc": {
		{"rpc.internal.tokens.get", "app/twitch/outgress/internal/tokenstore"},
		{"rpc.internal.tokens.save", "app/twitch/outgress/internal/tokenstore"},
		{"rpc.ingress.conduit.get", "app/twitch/outgress/internal/config ConduitSubject"},
		{"rpc.dashboard.state_get", "app/twitch/outgress/internal/config UsersStateSubject"},
		{"rpc.dashboard.identity_sync", "app/twitch/outgress/internal/config UsersIdentitySubject"},
		{"rpc.admin.notifications.send", "app/twitch/outgress/internal/config NotifySendSubject"},
	},
	"transactions_rpc": {
		{"rpc.internal.users.giveaway.pool", "app/db/transactions/giveaway"},
		{"rpc.internal.users.giveaway.coverage", "app/db/transactions/giveaway"},
		{"rpc.internal.users.giveaway.prepare", "app/db/transactions/giveaway"},
		{"rpc.internal.users.giveaway.commit", "app/db/transactions/giveaway"},
		{"rpc.internal.users.giveaway.cancel", "app/db/transactions/giveaway"},
		{"rpc.admin.user.auth.check", "app/db/transactions/giveaway"},
		{"rpc.internal.users.get", "app/db/transactions/main.go userGetSubject"},
		{"rpc.internal.billing.apply", "app/db/transactions/main.go billingSubject"},
		{"rpc.internal.users.email.get", "app/db/transactions/main.go emailSubject"},
		{"rpc.admin.notifications.send", "app/db/transactions/main.go sendSubject"},
	},
	"notifications_rpc": {
		{"rpc.internal.users.get", "app/db/notifications/main.go userGetSubject"},
	},
	"gossip_rpc": {
		{"rpc.internal.govee.key.get", "app/gossip/internal/core/goveekeys.go"},
		{"rpc.internal.spotify.key.get", "app/gossip/internal/core/spotifykeys.go"},
		{"rpc.internal.spotify.key.rotate", "app/gossip/internal/core/spotifykeys.go"},
		{"rpc.internal.spotify.key.dead", "app/gossip/internal/core/spotifykeys.go"},
		{"rpc.internal.commands.fetchkey.get", "app/gossip/internal/core/fetchkeys.go"},
		{"rpc.internal.projection.commands.fetches.get", "app/gossip/main.go newFetchProjection"},
	},
	"twitch_ingress_rpc": {
		{"rpc.broadcaster.status.get", "app/twitch/ingress broadcaster status lookup"},
		{"rpc.internal.users.get", "app/twitch/ingress/lib/ingress/trial_rpc.ex"},
		{"rpc.outgress.trial_subscription.create", "app/twitch/ingress/lib/ingress/trial_receiver.ex"},
		{"rpc.outgress.trial_subscription.delete", "app/twitch/ingress/lib/ingress/trial_receiver.ex"},
	},
	"dashboard_rpc": {
		{"rpc.automations.run", "web/dashboard/src/lib/server/automations-store.ts runNow"},
		{"rpc.automations.runs", "web/dashboard/src/lib/server/automations-store.ts listRuns"},
		{"rpc.transactions.giveaways.mine", "web/dashboard/src/lib/server/giveaways.ts"},
		{"rpc.dashboard.state_get", "web/dashboard SUB.dashboard"},
		{"rpc.dashboard.grant_save", "web/dashboard SUB.dashboard"},
		{"rpc.admin.user.audit.append", "web/dashboard SUB.audit"},
		{"rpc.delegation.list", "web/dashboard SUB.delegation"},
		{"rpc.commands.upsert", "web/dashboard SUB.commands"},
		{"rpc.commands.fetch_set_key", "web/dashboard SUB.commands"},
		{"rpc.modules.patch", "web/dashboard SUB.modules"},
		{"rpc.modules.govee.set", "web/dashboard SUB.goveeKey"},
		{"rpc.modules.spotify.app.set", "web/dashboard SUB.spotifyKey"},
		{"rpc.broadcaster.status.get", "web/dashboard SUB.broadcaster"},
		{"rpc.broadcaster.stream_info.get", "web/dashboard/src/lib/server/stream.ts"},
		{"rpc.projector.dashboard.commands.get", "web/dashboard/src/lib/server/commands-store.ts"},
		{"rpc.projector.dashboard.commands.replace", "web/dashboard/src/lib/server/commands-store.ts"},
		{"rpc.outgress.channel.get", "web/dashboard SUB.outgressRpc"},
		{"rpc.outgress.channelpoints.list", "web/dashboard SUB.outgressRpc"},
		{"rpc.outgress.accountage.get", "web/dashboard SUB.outgressRpc"},
		{"rpc.loyalty.counter.get", "web/dashboard SUB.loyalty"},
		{"rpc.loyalty.counter.board", "web/dashboard SUB.loyalty"},
		{"rpc.gossip.govee.devices", "web/dashboard SUB.gossip"},
		{"rpc.gossip.govee.scenes", "web/dashboard SUB.gossip"},
		{"rpc.gossip.spotify.exchange", "web/dashboard SUB.gossip"},
		{"rpc.gossip.spotify.playerqueue", "web/dashboard SUB.gossip"},
		{"rpc.gossip.spotify.next", "web/dashboard SUB.gossip"},
		{"rpc.gossip.custom.fetch", "web/dashboard SUB.gossip"},
		{"rpc.notifications.list", "web/dashboard SUB.notifications"},
		{"rpc.transactions.basket_create", "web/dashboard SUB.transactions"},
	},
	"admin_rpc": {
		{"rpc.admin.giveaways.create", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.preview", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.freeze", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.draw", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.get", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.list", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.retry", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.alerts", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.history", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.capabilities", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.include", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.giveaways.exclude", "web/admin/src/lib/server/giveaways.ts"},
		{"rpc.admin.premium.grant_email", "web/admin paid grant email"},
		{"rpc.admin.user.test.set", "web/admin user inspector"},
		{"rpc.admin.user.get", "web/admin"},
		{"rpc.admin.user.bot_token_set", "web/admin bot OAuth self-authorization"},
		{"rpc.admin.user.auth.check", "web/admin"},
		{"rpc.admin.analytics", "web/admin analytics"},
		{"rpc.health.admin", "web/admin health page"},
		{"rpc.admin.user.audit.list", "web/admin"},
		{"rpc.admin.notifications.send", "web/admin"},
		{"rpc.modules.personality.feed.board", "web/admin/src/lib/server/services.ts feedBoard"},
		{"rpc.loyalty.counter.get", "web/admin"},
		{"rpc.outgress.channel.get", "web/admin"},
		{"twitch.ingress.admin.shards.get", "web/admin"},
		{"twitch.ingress.admin.trials.list", "web/admin/src/lib/server/services.ts"},
		{"twitch.ingress.admin.trials.add", "web/admin/src/lib/server/services.ts"},
		{"twitch.ingress.admin.trials.set_enabled", "web/admin/src/lib/server/services.ts"},
		{"twitch.ingress.admin.trials.remove", "web/admin/src/lib/server/services.ts"},
		{"rpc.health.users", "web/admin health page"},
		{"rpc.broadcaster.live.list", "web/admin/src/lib/server/live-broadcasters.ts"},
		{"rpc.health.projector", "web/admin health page"},
		{"rpc.health.sesame", "web/admin health page"},
	},
}

var rpcServiceUsers = map[string]string{
	"app/db/admin":         "admin_service_rpc",
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
}

var rpcServicesWithoutIdentity = map[string]string{
	"app/plain":      "template service; config.Load() is called but no NATS connection is opened",
	"app/clusterops": "connects only as the system account's claims push user, never over RPC",
	"app/release":    "a GitHub Actions CLI; it never connects to NATS",
	"app/desktop":    "a desktop client; it never connects to NATS",
}

var rpcIntraAccountSubjects = map[string]map[string]string{
	"notifications_rpc": {
		"rpc.internal.notifications.cleanup": "the cleanup CronJob runs the service image with the service creds (app/db/notifications/cleanup.go)",
	},
}

var rpcUnusedDefaults = map[string]map[string]string{}

func TestRPCRequestsAreImportedAndExported(t *testing.T) {
	catalog := loadRPCCatalog(t)
	for _, user := range sortedManifestUsers() {
		requester, ok := catalog.byUser[user]
		if !assert.True(t, ok, "manifest names %s but accounts.yaml has no RPC account with that user", user) {
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
	assert.GreaterOrEqual(t, checked, 40, "the source scan likely stopped matching")
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
	if !assert.True(t, ok, "%s has a main.go but no entry in rpcServiceUsers: map it to its RPC user or list it in rpcServicesWithoutIdentity", service.name) {
		return rpcAccount{}, false
	}
	account, ok := c.byUser[user]
	assert.True(t, ok, "%s maps to user %s, which has no RPC account in accounts.yaml", service.name, user)
	return account, ok
}

func (c rpcCatalog) assertRequestGranted(t *testing.T, requester rpcAccount, req rpcRequest) {
	t.Helper()
	for _, subject := range []string{req.subject, req.subject + ".node.n1"} {
		assert.Empty(t, c.crossAccountProblem(requester, subject), "%s requests %s (%s)", requester.users[0], subject, req.source)
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
	assert.Empty(t, d.problem(), "%s loads %q", d.service, d.literal)
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
		require.NoError(t, err)
		mainFiles = append(mainFiles, matches...)
	}
	require.GreaterOrEqual(t, len(mainFiles), 10, "the glob no longer matches the service layout")
	services := make([]goService, 0, len(mainFiles))
	for _, mainFile := range mainFiles {
		dir := filepath.Dir(mainFile)
		name, err := filepath.Rel(root, dir)
		require.NoError(t, err)
		services = append(services, goService{name: filepath.ToSlash(name), dir: dir})
	}
	return services
}

func (g rpcGrant) covers(subject string) bool {
	return g.kind == "service" && server.SubjectsCollide(g.subject, subject)
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
	shared := confSubjectDefaults(t)
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
		for _, m := range confSubjectUsePattern.FindAllStringSubmatch(string(body), -1) {
			addSharedDefault(literals, shared, m[1])
		}
		return nil
	})
	require.NoError(t, err)
	return literals
}

func addSharedDefault(literals map[string]struct{}, shared map[string]string, name string) {
	if literal, ok := shared[name]; ok {
		literals[literal] = struct{}{}
	}
}

func confSubjectDefaults(t *testing.T) map[string]string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "pkg", "env", "conf", "subjects.go"))
	require.NoError(t, err)
	defaults := make(map[string]string)
	for _, m := range confSubjectDeclPattern.FindAllStringSubmatch(string(body), -1) {
		defaults[m[1]] = m[2]
	}
	require.NotEmpty(t, defaults, "the shared subject catalog scan stopped matching")
	return defaults
}

func isGoSource(d os.DirEntry, path string) bool {
	return !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}
