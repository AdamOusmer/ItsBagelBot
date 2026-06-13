package store

import (
	"context"
	"strings"

	"ItsBagelBot/pkg/cache"

	"github.com/newrelic/go-agent/v3/newrelic"

	"github.com/valkey-io/valkey-go"
)

const settingsKeyPrefix = "settings:"

// Valkey is the write side of the settings projection. One hash per user:
//
//	settings:<user_id>
//	  status                  free | paid | vip
//	  active                  0 | 1
//	  module:<name>:enabled   0 | 1
//	  module:<name>:config    raw JSON
//
// Readers get everything they need for a chat message in a single HGETALL,
// without parsing anything but the module config they actually use. Every
// write is an overwrite, so replays and redeliveries are harmless.
type Valkey struct {
	client valkey.Client
}

func NewValkey(address string, password string) (*Valkey, error) {

	valkeyOpts := valkey.ClientOption{
		InitAddress: []string{address},
		Password:    password,
		DisableCache: true,
	}
	if strings.HasSuffix(address, ":26379") {
		valkeyOpts.Sentinel = valkey.SentinelOption{MasterSet: "myprimary"}
	}

	client, err := valkey.NewClient(valkeyOpts)
	if err != nil {
		return nil, err
	}

	return &Valkey{client: client}, nil
}

// SetUser projects the tier status and active flag of one user.
func (v *Valkey) SetUser(ctx context.Context, userID uint64, status string, isActive bool) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	err := v.client.Do(ctx, v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("status", status).
		FieldValue("active", boolField(isActive)).
		Build(),
	).Error()
	if err != nil {
		return err
	}
	
	return v.client.Do(ctx, v.client.B().Expire().Key(key).Seconds(24*60*60).Build()).Error()
}

// GetUser retrieves the tier status and active flag of one user.
func (v *Valkey) GetUser(ctx context.Context, userID uint64) (string, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	res, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).Field("status").Field("active").Build()).AsStrSlice()
	if err != nil {
		return "", false, err
	}
	
	if len(res) < 2 {
		return "", false, nil
	}

	status := res[0]
	active := res[1] == "1"

	return status, active, nil
}

// SetModule projects one module row of one user.
func (v *Valkey) SetModule(ctx context.Context, userID uint64, name string, isEnabled bool, configs []byte) error {

	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("module:"+name+":enabled", boolField(isEnabled))

	if len(configs) > 0 {
		fields = fields.FieldValue("module:"+name+":config", string(configs))
	}

	if err := v.client.Do(ctx, fields.Build()).Error(); err != nil {
		return err
	}
	
	return v.client.Do(ctx, v.client.B().Expire().Key(key).Seconds(24*60*60).Build()).Error()
}

// DeleteUser drops the whole projection of one user.
func (v *Valkey) DeleteUser(ctx context.Context, userID uint64) error {

	defer segment(ctx, "DEL")()

	key := cache.UserKey(settingsKeyPrefix, userID)

	return v.client.Do(ctx, v.client.B().Del().Key(key).Build()).Error()
}

// Close releases the connection pool.
func (v *Valkey) Close() {
	v.client.Close()
}

func boolField(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// segment reports the operation as a datastore segment of the transaction in
// ctx. New Relic has no Valkey product constant, so it reports under Redis,
// which is wire-compatible anyway. Without a transaction this is a no-op.
func segment(ctx context.Context, operation string) func() {

	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return func() {}
	}

	seg := &newrelic.DatastoreSegment{
		StartTime:  txn.StartSegmentNow(),
		Product:    newrelic.DatastoreRedis,
		Collection: "settings",
		Operation:  operation,
	}

	return seg.End
}
