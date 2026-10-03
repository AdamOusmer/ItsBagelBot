// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeHash map[string]string

type fakeField struct {
	field string
	value string
}

type fakeValkey struct{ *valkeytest.Server }

func newFakeValkey(t *testing.T) *fakeValkey {
	t.Helper()
	srv := valkeytest.New(t)
	srv.Handle("EVAL", execEVAL)
	srv.Handle("EVAL_RO", execEVALRO)
	return &fakeValkey{srv}
}

func newTestStore(t *testing.T) (*Store, *fakeValkey) {
	t.Helper()
	f := newFakeValkey(t)
	return NewStore(f.Client()), f
}

type evictFunc func(scope string, userID uint64, keys ...string)

func invalidatedClient(t *testing.T, store *Store) (*Client, evictFunc) {
	t.Helper()
	nc := testnats.Connect(t)
	const unanswered = "projection.unanswered"
	c := NewClient(Config{
		Store: store, NC: nc, TTL: time.Minute, Log: zap.NewNop(),
		Subjects: Subjects{Users: unanswered, Modules: unanswered, Commands: unanswered, Fetches: unanswered},
	})
	c.StartInvalidationListener("inv")
	t.Cleanup(c.Close)
	return c, func(scope string, userID uint64, keys ...string) {
		t.Helper()
		require.NoError(t, invalidate.PublishKeys(nc, "inv", scope, strconv.FormatUint(userID, 10), keys...))
		require.NoError(t, nc.Flush())
	}
}

func (f *fakeValkey) failHGET(field string) {
	if field == "" {
		f.Heal("HGET")
		return
	}
	f.Fail(valkeytest.Failure{Cmd: "HGET", Message: "SIMULATED transient read failure", Match: func(a valkeytest.Args) bool { return a[1] == field }})
}

func (f *fakeValkey) seed(key string, kv fakeField) {
	f.With(func(s *valkeytest.Server) {
		if s.Hashes[key] == nil {
			s.Hashes[key] = map[string]string{}
		}
		s.Hashes[key][kv.field] = kv.value
	})
}

func (f *fakeValkey) hash(key string) fakeHash {
	var out fakeHash
	f.With(func(s *valkeytest.Server) { out = maps.Clone(s.Hashes[key]) })
	return out
}

func execEVALRO(s *valkeytest.Server, args valkeytest.Args) valkeytest.Reply {
	if args[0] != moduleSnapshotRead {
		return valkeytest.Err("unsupported read-only script")
	}
	key := args[2]
	flat := []string{}
	if s.Alive(key) {
		for name, value := range s.Hashes[key] {
			if name == modulesMarkerField || strings.HasPrefix(name, moduleFieldPrefix) {
				flat = append(flat, name, value)
			}
		}
	}
	return valkeytest.Flat(flat)
}

func execEVAL(s *valkeytest.Server, args valkeytest.Args) valkeytest.Reply {
	script := args[0]
	numkeys, _ := strconv.Atoi(args[1])
	keys := args[2 : 2+numkeys]
	argv := args[2+numkeys:]
	if strings.Contains(script, "user watch admission write") {
		return execUserAdmissionWrite(s, keys, argv)
	}
	if strings.Contains(script, "user watch deletion") {
		return execUserWatchDeletion(s, keys)
	}
	if strings.Contains(script, "module revision gate") {
		return execModuleRevisionGate(s, keys, argv)
	}
	if strings.Contains(script, "module hydration revision seed") {
		return execModuleHydrationSeed(s, keys, argv)
	}
	if !strings.Contains(script, "HKEYS") || !strings.Contains(script, "HDEL") {
		return valkeytest.Err("unsupported script in fake")
	}
	return valkeytest.Int(deleteByPrefixes(s, keys[0], argv))
}

func execUserAdmissionWrite(s *valkeytest.Server, keys, argv valkeytest.Args) valkeytest.Reply {
	if !userAdmissionWriteAllowed(s, keys[1], argv) {
		return valkeytest.Int(0)
	}
	applyUserAdmissionWrite(s, keys[0], keys[1], argv)
	return valkeytest.Int(1)
}

func userAdmissionWriteAllowed(s *valkeytest.Server, admission string, argv valkeytest.Args) bool {
	if s.Hashes[admission]["deleted"] == "1" {
		return false
	}
	if instance := s.Hashes[admission]["instance"]; instance != "" && instance != argv[6] {
		return false
	}
	previous, _ := strconv.ParseInt(s.Hashes[admission]["state_revision"], 10, 64)
	incoming, _ := strconv.ParseInt(argv[7], 10, 64)
	return incoming >= previous
}

func applyUserAdmissionWrite(s *valkeytest.Server, settings, admission string, argv valkeytest.Args) {
	if s.Hashes[settings] == nil {
		s.Hashes[settings] = fakeHash{}
	}
	if s.Hashes[admission] == nil {
		s.Hashes[admission] = fakeHash{}
	}
	incoming, _ := strconv.ParseInt(argv[7], 10, 64)
	if incoming > 0 {
		s.Hashes[admission]["state_revision"] = argv[7]
	}
	if s.Hashes[settings]["active"] != argv[0] || s.Hashes[settings]["banned"] != argv[1] {
		n, _ := strconv.Atoi(s.Hashes[admission]["epoch"])
		s.Hashes[admission]["epoch"] = strconv.Itoa(n + 1)
	}
	for i, name := range []string{"active", "banned", "status", "commands_page_hidden"} {
		s.Hashes[settings][name] = argv[i]
	}
	if argv[4] != "" {
		s.Hashes[settings]["locale"] = argv[4]
	}
	secs, _ := strconv.Atoi(argv[5])
	s.Expires[settings] = s.Now().Add(time.Duration(secs) * time.Second)
}

func execUserWatchDeletion(s *valkeytest.Server, keys valkeytest.Args) valkeytest.Reply {
	current, _ := strconv.ParseInt(s.Hashes[keys[1]]["instance"], 10, 64)
	if current > 0 {
		return valkeytest.Int(0)
	}
	delete(s.Hashes, keys[0])
	delete(s.Expires, keys[0])
	if s.Hashes[keys[1]] == nil {
		s.Hashes[keys[1]] = fakeHash{}
	}
	n, _ := strconv.Atoi(s.Hashes[keys[1]]["epoch"])
	s.Hashes[keys[1]]["epoch"] = strconv.Itoa(n + 1)
	s.Hashes[keys[1]]["deleted"] = "1"
	return valkeytest.Int(1)
}

func execModuleRevisionGate(s *valkeytest.Server, keys, argv valkeytest.Args) valkeytest.Reply {
	if len(argv) != 8 {
		return valkeytest.Err("invalid module gate")
	}
	if !moduleRevisionWriteAllowed(s, keys[0], keys[1], argv) {
		return valkeytest.Int(0)
	}
	applyModuleRevisionWrite(s, keys[0], argv)
	return valkeytest.Int(1)
}

func moduleRevisionWriteAllowed(s *valkeytest.Server, settings, admission string, argv valkeytest.Args) bool {
	if s.Hashes[admission]["deleted"] == "1" {
		return false
	}
	if !moduleInstanceMatches(s, admission, argv[2], argv[7]) {
		return false
	}
	incoming, _ := strconv.Atoi(argv[1])
	current, _ := strconv.Atoi(s.Hashes[settings][argv[0]])
	_, settingsExist := s.Hashes[settings]
	return !settingsExist || incoming >= current
}

func moduleInstanceMatches(s *valkeytest.Server, admission, field, incoming string) bool {
	if field != "module:loyalty:enabled" {
		return true
	}
	instance := s.Hashes[admission]["instance"]
	if instance == "" {
		return true
	}
	return instance == incoming
}

func applyModuleRevisionWrite(s *valkeytest.Server, settings string, argv valkeytest.Args) {
	if s.Hashes[settings] == nil {
		s.Hashes[settings] = fakeHash{}
	}
	s.Hashes[settings][argv[0]] = argv[1]
	s.Hashes[settings][argv[2]] = argv[3]
	s.Hashes[settings][argv[4]] = argv[5]
	s.Hashes[settings][strings.TrimSuffix(argv[2], ":enabled")+":account_created_at"] = argv[7]
}

func execModuleHydrationSeed(s *valkeytest.Server, keys, argv valkeytest.Args) valkeytest.Reply {
	if len(argv) < 2 {
		return valkeytest.Err("invalid module hydration")
	}
	n, err := strconv.Atoi(argv[1])
	if err != nil || len(argv) != 2+5*n {
		return valkeytest.Err("invalid module hydration")
	}
	if s.Hashes[keys[0]] == nil {
		s.Hashes[keys[0]] = fakeHash{}
	}
	for i := 0; i < n; i++ {
		seedHydratedModule(s, keys[0], argv, i)
	}
	s.Hashes[keys[0]]["modules:projected"] = "1"
	return valkeytest.Int(1)
}

func seedHydratedModule(s *valkeytest.Server, key string, argv valkeytest.Args, index int) {
	position := 2 + index*5
	name := argv[position]
	incoming, _ := strconv.Atoi(argv[position+1])
	current, _ := strconv.Atoi(s.Hashes[key]["module:"+name+":revision"])
	if incoming < current {
		return
	}
	s.Hashes[key]["module:"+name+":revision"] = argv[position+1]
	s.Hashes[key]["module:"+name+":enabled"] = argv[position+2]
	s.Hashes[key]["module:"+name+":config"] = argv[position+3]
	s.Hashes[key]["module:"+name+":account_created_at"] = argv[position+4]
}

func deleteByPrefixes(s *valkeytest.Server, key string, prefixes valkeytest.Args) int64 {
	h, ok := s.Hashes[key]
	if !ok || !s.Alive(key) {
		return 0
	}
	removed := int64(0)
	for field := range h {
		if hasAnyPrefix(field, prefixes) {
			delete(h, field)
			removed++
		}
	}
	return removed
}

func hasAnyPrefix(s string, prefixes valkeytest.Args) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
