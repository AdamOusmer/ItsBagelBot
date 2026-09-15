// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import "testing"

func TestGiveawayGrantCustody(t *testing.T) {
	catalog := loadRPCCatalog(t)
	for _, verb := range []string{"pool", "coverage", "prepare", "commit", "cancel"} {
		subject := "bagel.rpc.internal.users.giveaway." + verb
		for _, account := range catalog.accounts {
			if account.name == "TRANSACTIONS_RPC" || account.name == "USERS_RPC" {
				continue
			}
			for _, candidate := range []string{subject, subject + ".node.n1"} {
				if _, ok := account.importCovering(candidate); ok {
					t.Errorf("%s must not import giveaway eligibility or grant custody: %s", account.name, candidate)
				}
			}
		}
	}
}

func TestDashboardCannotAdministerGiveaways(t *testing.T) {
	catalog := loadRPCCatalog(t)
	dashboard := catalog.byUser["dashboard_rpc"]
	for _, verb := range []string{"create", "preview", "freeze", "draw", "get", "list", "retry", "alerts", "history", "capabilities"} {
		subject := "bagel.rpc.admin.giveaways." + verb
		for _, candidate := range []string{subject, subject + ".node.n1"} {
			if _, ok := dashboard.importCovering(candidate); ok {
				t.Errorf("dashboard can reach privileged giveaway subject %s", candidate)
			}
		}
	}
	if problem := catalog.crossAccountProblem(dashboard, "bagel.rpc.transactions.giveaways.mine"); problem != "" {
		t.Fatal(problem)
	}
}
