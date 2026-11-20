// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"slices"
	"strings"
	"testing"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/natsacl"

	"github.com/stretchr/testify/assert"
)

func TestChannelGiveawayWinnersRPCIsolation(t *testing.T) {
	verbs := []string{
		modulesrpc.VerbGiveawayRecord, modulesrpc.VerbGiveawayDeliver,
		modulesrpc.VerbGiveawayRecent, modulesrpc.VerbGiveawayWonWithin,
	}
	for _, account := range loadRPCCatalog(t).accounts {
		for _, verb := range verbs {
			assertGiveawayImport(t, account, "rpc.modules"+modulesrpc.GiveawaySuffix+"."+verb)
		}
	}
}

func assertGiveawayImport(t *testing.T, account rpcAccount, subject string) {
	t.Helper()
	allowed := account.name == "DASHBOARD_RPC" || account.name == "WORKER_RPC"
	for _, candidate := range []string{subject, subject + ".node.n1"} {
		grant, imported := account.importCovering(candidate)
		assert.Equal(t, allowed, imported, "%s: %s", account.name, candidate)
		if imported {
			assert.Equal(t, "MODULES_RPC", grant.account)
		}
	}
}

func TestGiveawayGrantCustody(t *testing.T) {
	for _, verb := range []string{"pool", "coverage", "prepare", "commit", "cancel"} {
		subject := "rpc.internal.users.giveaway." + verb
		for _, account := range loadRPCCatalog(t).accounts {
			if account.name == "TRANSACTIONS_RPC" || account.name == "USERS_RPC" {
				continue
			}
			for _, candidate := range []string{subject, subject + ".node.n1"} {
				_, imported := account.importCovering(candidate)
				assert.False(t, imported, "%s must not import giveaway eligibility or grant custody: %s", account.name, candidate)
			}
		}
	}
}

func TestAnalyticsLevelsOnlyImportedByAdminService(t *testing.T) {
	for _, service := range []string{"users", "modules", "commands"} {
		for _, account := range loadRPCCatalog(t).accounts {
			for _, suffix := range []string{"", ".node.n1"} {
				subject := "rpc.internal." + service + ".analytics.levels" + suffix
				_, imported := account.importCovering(subject)
				assert.Equal(t, account.name == "ADMIN_SERVICE_RPC", imported, "%s: %s", account.name, subject)
			}
		}
	}
}

func TestDashboardCannotAdministerGiveaways(t *testing.T) {
	catalog := loadRPCCatalog(t)
	dashboard := catalog.byUser["dashboard_rpc"]
	subjects := []string{"rpc.admin.premium.grant_email"}
	for _, verb := range []string{"create", "preview", "freeze", "draw", "get", "list", "retry", "alerts", "history", "capabilities", "include", "exclude"} {
		subjects = append(subjects, "rpc.admin.giveaways."+verb)
	}
	for _, subject := range subjects {
		for _, candidate := range []string{subject, subject + ".node.n1"} {
			_, imported := dashboard.importCovering(candidate)
			assert.False(t, imported, "dashboard can reach privileged subject %s", candidate)
		}
	}

	assert.Empty(t, catalog.crossAccountProblem(dashboard, "rpc.transactions.giveaways.mine"))
}

func TestExactRPCGrantsIncludeNodeLocalVariant(t *testing.T) {
	counts := map[string]int{}
	for _, spec := range committedACL(t).Accounts {
		for _, subject := range serviceSubjects(spec) {
			counts[subject]++
		}
	}
	exact := exactGrants(counts)

	for subject, count := range exact {
		local := subject + ".node.*"
		assert.GreaterOrEqual(t, counts[local], count, "exact service grant %q occurs %d times; local grant %q occurs %d times",
			subject, count, local, counts[local])
	}
	assert.GreaterOrEqual(t, len(exact), 10, "accounts.yaml likely lost its service grants")
}

func exactGrants(counts map[string]int) map[string]int {
	exact := map[string]int{}
	for subject, count := range counts {
		if !strings.HasSuffix(subject, ".>") && !strings.HasSuffix(subject, ".node.*") {
			exact[subject] = count
		}
	}
	return exact
}

func serviceSubjects(spec natsacl.AccountSpec) []string {
	var subjects []string
	for _, exp := range spec.Exports {
		subjects = append(subjects, exp.Service)
	}
	for _, imp := range spec.Imports {
		subjects = append(subjects, imp.Service)
	}
	return slices.DeleteFunc(subjects, func(s string) bool { return s == "" })
}
