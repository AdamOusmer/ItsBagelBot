// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import (
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestCommandsPageSetRequestDecodesConsolePayload(t *testing.T) {
	var req CommandsPageSetRequest
	require.NoError(t, codec.Unmarshal([]byte(`{"broadcaster_user_id":"1001","commands_page_hidden":true}`), &req))
	require.Equal(t, CommandsPageSetRequest{BroadcasterUserID: "1001", Hidden: true}, req)
}
