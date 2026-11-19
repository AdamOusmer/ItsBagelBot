// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationPagesFitMessagingPayload(t *testing.T) {
	_, _, repo := setupAutomations(t, "automation-pages")
	chat := `{"type":"chat","params":{"text":"` + strings.Repeat("<", 220) + `"}}`
	actions := strings.TrimSuffix(strings.Repeat(chat+",", automation.MaxActions), ",")
	raw := []byte(`{"version":1,"trigger":{"type":"manual"},"actions":[` + actions + `]}`)
	require.Less(t, len(raw), automation.MaxDefinitionBytes)
	for i := range 9 {
		_, err := repo.Upsert(t.Context(), 7, repository.AutomationInput{Name: fmt.Sprintf("Recipe %02d", i), Definition: raw})
		require.NoError(t, err)
	}
	for _, tc := range []struct{ page, count, next int }{{0, 9, 0}, {1, 4, 2}, {2, 4, 3}, {3, 1, 0}, {4, 0, 0}} {
		t.Run(fmt.Sprint(tc.page), func(t *testing.T) {
			items, next, err := repo.ListPage(t.Context(), 7, tc.page)
			require.NoError(t, err)
			assert.Equal(t, []int{tc.count, tc.next}, []int{len(items), next})
			assertAutomationPageSize(t, automationrpc.ListReply{Automations: items, NextPage: next}, tc.page)
		})
	}
}

func assertAutomationPageSize(t *testing.T, reply automationrpc.ListReply, page int) {
	t.Helper()
	wire, err := codec.FastMarshal(reply)
	require.NoError(t, err)
	t.Logf("page=%d rows=%d bytes=%d", page, len(reply.Automations), len(wire))
	if page == 0 {
		assert.Greater(t, len(wire), 8*1024*1024)
		return
	}
	assert.Less(t, len(wire), 8*1024*1024)
}
