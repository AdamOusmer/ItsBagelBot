// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"testing"

	"github.com/nats-io/jwt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverlayDeviceAuthExportsStayOutsideDashboardAuthority(t *testing.T) {
	catalog := loadRPCCatalog(t)
	users := catalog.byUser["users_rpc"]
	for _, verb := range []string{"token", "authorize", "revoke"} {
		subject := "bagel.rpc.overlay.device." + verb
		for _, candidate := range []string{subject, subject + ".node.n1"} {
			assert.True(t, users.exportsCoverOrDescend(candidate), "Users must export the overlay auth operation %s", candidate)
			for _, account := range catalog.accounts {
				_, imported := account.importCovering(candidate)
				assert.False(t, imported, "%s must not inherit the future overlay auth identity: %s", account.name, candidate)
			}
		}
	}
}

func TestPreloadedDashboardCannotImportOverlayDeviceAuth(t *testing.T) {
	dashboard := committedPreload(t)[committedKeys(t).Accounts["DASHBOARD_RPC"]]
	require.NotNil(t, dashboard)
	for _, verb := range []string{"token", "authorize", "revoke"} {
		subject := "bagel.rpc.overlay.device." + verb
		for _, candidate := range []string{subject, subject + ".node.n1"} {
			for _, imported := range dashboard.Imports {
				if imported.Type != jwt.Service {
					continue
				}
				grant := rpcGrant{kind: "service", subject: string(imported.Subject)}
				assert.False(t, grant.covers(candidate), "committed dashboard identity imports overlay auth: %s", candidate)
			}
		}
	}
}

func TestDashboardKeepsApprovalAndGrantManagement(t *testing.T) {
	catalog := loadRPCCatalog(t)
	dashboard := catalog.byUser["dashboard_rpc"]
	for _, verb := range []string{"create_code", "list", "revoke", "revoke_all"} {
		subject := "bagel.rpc.dashboard.device." + verb
		for _, candidate := range []string{subject, subject + ".node.n1"} {
			assert.Empty(t, catalog.crossAccountProblem(dashboard, candidate), "dashboard must retain consent and account management: %s", candidate)
		}
	}
}
