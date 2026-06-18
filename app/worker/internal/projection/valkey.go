package projection

import (
	"context"
	"strings"

	"ItsBagelBot/pkg/cache"

	"github.com/valkey-io/valkey-go"
)

const settingsKeyPrefix = "settings:"

// Valkey is the worker's read-only view of the settings projection the
// projector writes (see app/projector/store). The worker never writes it: the
// projector is the only writer, so on a cold key the worker falls through to
// the projector's RPC and lets the projector populate Valkey. The layout is
// one hash per user:
//
//	settings:<user_id>
//	  status                  free | paid | vip | premium
//	  active                  0 | 1
//	  live                    0 | 1
//	  module:<name>:enabled   0 | 1
//	  module:<name>:config    raw JSON
//
// A whole user's modules come back in one HGETALL, which is all the pipeline
// needs to decide what to run.
type Valkey struct {
	client valkey.Client
}

func NewValkey(address, password string) (*Valkey, error) {
	opts := valkey.ClientOption{
		InitAddress:  []string{address},
		Password:     password,
		DisableCache: true,
	}
	if strings.HasSuffix(address, ":26379") {
		opts.Sentinel = valkey.SentinelOption{MasterSet: "myprimary"}
	}

	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, err
	}
	return &Valkey{client: client}, nil
}

func (v *Valkey) Close() { v.client.Close() }

// GetUser reads the tier status, active flag and live flag. An empty status means the
// user is not projected yet (cold cache), which the caller treats as a miss.
func (v *Valkey) GetUser(ctx context.Context, userID uint64) (string, bool, bool, error) {
	key := cache.UserKey(settingsKeyPrefix, userID)

	res, err := v.client.Do(ctx, v.client.B().Hmget().Key(key).Field("status").Field("active").Field("live").Build()).AsStrSlice()
	if err != nil {
		return "", false, false, err
	}
	if len(res) < 3 {
		return "", false, false, nil
	}
	return res[0], res[1] == "1", res[2] == "1", nil
}

// GetModules reads every module row of one user out of the settings hash. An
// empty map means the user is not projected yet, which the caller treats as a
// miss and resolves over RPC.
func (v *Valkey) GetModules(ctx context.Context, userID uint64) (map[string]Module, error) {
	key := cache.UserKey(settingsKeyPrefix, userID)

	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, err
	}

	mods := map[string]Module{}
	for field, value := range fields {
		name, suffix, ok := parseModuleField(field)
		if !ok {
			continue
		}
		m := mods[name]
		m.Name = name
		switch suffix {
		case "enabled":
			m.IsEnabled = value == "1"
		case "config":
			m.Configs = []byte(value)
		}
		mods[name] = m
	}
	return mods, nil
}

// parseModuleField splits "module:<name>:enabled" / "module:<name>:config"
// into the module name and the suffix. A name may itself contain colons, so
// the suffix is taken from the end.
func parseModuleField(field string) (name, suffix string, ok bool) {
	rest, found := strings.CutPrefix(field, "module:")
	if !found {
		return "", "", false
	}
	idx := strings.LastIndex(rest, ":")
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}
