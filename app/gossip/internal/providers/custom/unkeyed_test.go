package custom

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// An unkeyed definition (empty key_label) must never consult the key
// resolver: the default harness wires no FetchKeys at all, so any lookup
// would surface as errKeyMissing and a bad_def reply. The upstream echoes
// the Authorization header it received so the test can prove nothing was
// attached, rather than only that the fetch succeeded.
func TestUnkeyedDefFetchesWithoutKeyResolver(t *testing.T) {
	h := newHarness(t)
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"auth":"` + r.Header.Get(authHeaderName) + `"}`))
	}))
	t.Cleanup(echo.Close)

	h.defs["open"] = gossiprpc.FetchDef{
		Name: "open", URL: echo.URL + "/echo", IsActive: true, KeyLabel: "", JSONPath: []string{"auth"},
	}

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "open"})
	require.Equal(t, gossiprpc.FetchOK, reply.Status, "unkeyed def must fetch with no resolver wired")
	require.Equal(t, []string{""}, reply.Values, "no Authorization header may ride an unkeyed fetch")
}
