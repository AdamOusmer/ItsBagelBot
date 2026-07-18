package valkey

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	valkey_go "github.com/valkey-io/valkey-go"
)

func TestValkeyTelemetryResultsStayFinite(t *testing.T) {
	assert.Equal(t, "ok", valkeyResult(nil))
	assert.Equal(t, "miss", valkeyResult(valkey_go.Nil))
	assert.Equal(t, "error", valkeyResult(errors.New("unavailable")))
}
