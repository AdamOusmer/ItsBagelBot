package main

import (
	"ItsBagelBot/app/twitch/outgress/internal/worker"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

// Match the actual publisher and deployed subscriber permission. A control
// subject outside this namespace silently prevents automatic unban recovery.
func TestModerationControlPublisherFitsDeployedAuthzPermission(t *testing.T) {
	publisher, err := os.ReadFile("../ingress/lib/ingress/shard_session.ex")
	require.NoError(t, err)
	require.Contains(t, string(publisher), `Nats.publish("`+worker.BotModerationSubject+`",`)
	permissions, err := os.ReadFile("../../../deploy/messaging/accounts.yaml")
	require.NoError(t, err)
	require.Contains(t, string(permissions), "- twitch.ingress.status.authz.>")
	require.True(t, strings.HasPrefix(worker.BotModerationSubject, "twitch.ingress.status.authz."))
}
