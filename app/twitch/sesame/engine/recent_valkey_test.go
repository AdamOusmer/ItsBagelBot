// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newRecentStore(t *testing.T) (*ValkeyRecent, *fakeValkey) {
	t.Helper()
	f := newFakeValkey(t)
	return NewValkeyRecent(f.client, zap.NewNop()), f
}

func flushRecent(t *testing.T, v *ValkeyRecent) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		v.Start(ctx)
		close(done)
	}()
	cancel()
	<-done
}

func TestValkeyRecentFlushPipelinesPerChannelShape(t *testing.T) {
	v, f := newRecentStore(t)

	v.Record(123, soloChatEnv("999", "hello there"), nukeClockBase)
	v.Record(123, cohortChatEnv([]string{"555", "556"}, "same copypasta everywhere"), nukeClockBase.Add(time.Second))
	v.Record(456, soloChatEnv("777", "other channel line"), nukeClockBase)
	flushRecent(t, v)

	byKey := map[string][][]string{}
	for _, cmd := range f.commands() {
		require.GreaterOrEqual(t, len(cmd), 2)
		byKey[cmd[1]] = append(byKey[cmd[1]], cmd)
	}
	require.Len(t, byKey, 2, "one pipelined group per touched channel")

	cutoff := strconv.FormatInt((nukeClockBase.Add(time.Second).UnixNano()-int64(recentTTL))/int64(time.Millisecond), 10)
	assert.Equal(t, [][]string{
		{
			"ZADD", "am:recent:123",
			strconv.FormatInt(nukeClockBase.UnixMilli(), 10), "999:0:hello there",
			strconv.FormatInt(nukeClockBase.Add(time.Second).UnixMilli(), 10), "555:0:same copypasta everywhere",
			strconv.FormatInt(nukeClockBase.Add(time.Second).UnixMilli(), 10), "556:0:same copypasta everywhere",
		},
		{"ZREMRANGEBYSCORE", "am:recent:123", "-inf", cutoff},
		{"ZREMRANGEBYRANK", "am:recent:123", "0", strconv.Itoa(-(recentRingCap + 1))},
		{"EXPIRE", "am:recent:123", strconv.FormatInt(int64(recentTTL/time.Second), 10)},
	}, byKey["am:recent:123"])
	assert.Equal(t, "am:recent:456", byKey["am:recent:456"][0][1], "the second channel is tenant-scoped")
}

func TestValkeyRecentFlushSkipsWhatIsNotRetainable(t *testing.T) {
	v, f := newRecentStore(t)
	flushRecent(t, v)

	v.Record(123, soloChatEnv("999", "!nuke spam"), nukeClockBase)
	v.Record(123, soloChatEnv("999", ""), nukeClockBase)
	flushRecent(t, v)

	assert.Empty(t, f.commands(), "nothing retainable means no round trip")
}

type sweepHit struct {
	user channelID
	role module.Role
}

func TestValkeyRecentSweepReadsOneBoundedRange(t *testing.T) {
	cases := []struct {
		name    string
		phrase  string
		members []string
		want    []sweepHit
	}{
		{
			name: "matches parse, normalize and dedupe while garbage is skipped",
			members: []string{
				"111:0:join my FREE N1TRO giveaway",
				"111:0:free nitro again!!",
				"222:4:free nitro is my whole personality",
				"garbage-without-colons",
				"zero:0:no uid",
			},
			want: []sweepHit{{111, module.RoleEveryone}, {222, module.RoleLeadModerator}},
		},
		{
			name:   "a stored member carries its sender and role and malformed members are skipped",
			phrase: "kekw kekw",
			members: []string{
				"44322889:2:KEKW KEKW :D",
				"", "nocolons", "abc:0:kekw kekw", "44322889:x:kekw kekw", "0:0:kekw kekw", "44322889:", "44322889",
			},
			want: []sweepHit{{44322889, module.RoleVIP}},
		},
		{
			name: "one hit per sender is kept in first-seen order",
			members: []string{
				"111:0:free nitro, first", "222:0:free nitro too", "111:0:free nitro, again",
				"333:0:free nitro three", "222:0:free nitro once more", "111:0:free nitro, still",
			},
			want: []sweepHit{{111, module.RoleEveryone}, {222, module.RoleEveryone}, {333, module.RoleEveryone}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, f := newRecentStore(t)
			f.scriptRange(tc.members)

			var got []sweepHit
			for _, hit := range v.Sweep(context.Background(), 123, cmp.Or(tc.phrase, "free nitro"), nukeClockBase) {
				got = append(got, sweepHit{hit.UserID, hit.Role})
			}

			assert.Equal(t, tc.want, got)
			wantMin := strconv.FormatInt(nukeClockBase.Add(-recentTTL).UnixMilli(), 10)
			assert.Equal(t,
				[][]string{{"ZRANGEBYSCORE", "am:recent:123", wantMin, "+inf", "LIMIT", "0", strconv.Itoa(recentFetchLimit)}},
				f.commands(), "the cutoff rides the command and nothing is written")
		})
	}
}

func TestValkeyRecentNilClientDegradesSilently(t *testing.T) {
	v := NewValkeyRecent(nil, zap.NewNop())
	v.Record(123, soloChatEnv("999", "free nitro everyone"), nukeClockBase)
	assert.Empty(t, v.Sweep(context.Background(), 123, "free nitro", nukeClockBase))
}

func TestValkeyRecentEntriesRecordedAfterAFailedFlushStillLand(t *testing.T) {
	v, f := newRecentStore(t)
	f.goDown()
	v.Record(123, soloChatEnv("999", "hello world again"), nukeClockBase)
	flushRecent(t, v)

	f.comeBack()
	v.Record(123, soloChatEnv("998", "second line here"), nukeClockBase)
	flushRecent(t, v)

	var members []string
	for _, cmd := range f.commands() {
		if cmd[0] == "ZADD" {
			members = append(members, cmd[3:]...)
		}
	}
	assert.Contains(t, members, "998:0:second line here")
}
