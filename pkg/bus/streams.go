// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
)

type StreamSpec struct {
	Name         string
	Subjects     []string
	Retention    nats.RetentionPolicy
	MaxAge       time.Duration
	MaxBytes     int64
	MaxMsgsPer   int64
	Duplicates   time.Duration
	Storage      nats.StorageType
	BatchPublish bool
	MsgSchedules bool
	Replicas     int
	// Never change tags and Replicas in one edit: the server rejects a move combined with a scale.
	PlacementTags []string
}

var OutgressStream = StreamSpec{
	Name:         "TWITCH_OUTGRESS",
	Subjects:     []string{"twitch.outgress.premium", "twitch.outgress.standard"},
	Retention:    nats.WorkQueuePolicy,
	MaxAge:       5 * time.Second,
	MaxBytes:     256 << 20,
	Storage:      nats.MemoryStorage,
	BatchPublish: true,
	Replicas:     3,
}

var OutgressSystemStream = StreamSpec{
	Name:         "TWITCH_OUTGRESS_SYSTEM",
	Subjects:     []string{"twitch.outgress.system"},
	Retention:    nats.WorkQueuePolicy,
	MaxAge:       5 * time.Minute,
	MaxBytes:     64 << 20,
	Storage:      nats.FileStorage,
	BatchPublish: true,
	Replicas:     3,
}

var BagelDataStream = StreamSpec{
	Name:         "BAGEL_DATA",
	Subjects:     []string{"data.>"},
	MaxAge:       5 * time.Minute,
	MaxBytes:     512 << 20,
	Storage:      nats.FileStorage,
	BatchPublish: true,
	Replicas:     3,
}

var TwitchIngressStream = StreamSpec{
	Name: "TWITCH_INGRESS",
	Subjects: []string{
		// Enumerated, not twitch.ingress.event.>: two streams may not claim one subject.
		"twitch.ingress.event.premium",
		"twitch.ingress.event.stream",
		"twitch.ingress.status.>",
	},
	MaxAge:       10 * time.Second,
	Storage:      nats.MemoryStorage,
	Replicas:     3,
	MaxBytes:     384 << 20,
	MaxMsgsPer:   400_000,
	Duplicates:   10 * time.Second,
	BatchPublish: true,
}

// Reconcile after TwitchIngressStream wherever both are listed; the reverse order fails boot.
var TwitchIngressStandardStream = StreamSpec{
	Name: "TWITCH_INGRESS_STANDARD",
	// Exactly the subject TwitchIngressStream no longer enumerates.
	Subjects:     []string{"twitch.ingress.event.standard"},
	MaxAge:       10 * time.Second,
	Storage:      nats.MemoryStorage,
	Replicas:     3,
	MaxBytes:     640 << 20,
	Duplicates:   10 * time.Second,
	BatchPublish: true,
}

var TwitchIngressRetryStream = StreamSpec{
	Name:     "TWITCH_INGRESS_RETRY",
	Subjects: []string{"twitch.ingress.retry.>"},
	// Must outlive the retry delay: an evicted schedule row silently cancels the retry.
	MaxAge:       2 * time.Minute,
	MaxBytes:     32 << 20,
	Storage:      nats.MemoryStorage,
	Replicas:     3,
	MsgSchedules: true,
	BatchPublish: true,
}

var DiscordIngressStream = StreamSpec{
	Name: "DISCORD_INGRESS",
	Subjects: []string{
		"discord.ingress.event.message",
		"discord.ingress.event.member",
		"discord.ingress.event.voice",
		"discord.ingress.event.interaction",
		"discord.ingress.event.audit",
		"discord.ingress.event.guild",
	},
	MaxAge:       60 * time.Second,
	MaxBytes:     128 << 20,
	Storage:      nats.MemoryStorage,
	BatchPublish: true,
	Replicas:     3,
}

var DiscordOutgressStream = StreamSpec{
	Name:         "DISCORD_OUTGRESS",
	Subjects:     []string{"discord.outgress.mod", "discord.outgress.default"},
	Retention:    nats.WorkQueuePolicy,
	MaxAge:       60 * time.Second,
	MaxBytes:     64 << 20,
	Storage:      nats.MemoryStorage,
	BatchPublish: true,
	Replicas:     3,
}

var YouTubeOutgressStream = StreamSpec{
	Name:         "YOUTUBE_OUTGRESS",
	Subjects:     []string{"youtube.outgress.premium", "youtube.outgress.standard"},
	Retention:    nats.WorkQueuePolicy,
	MaxAge:       5 * time.Second,
	MaxBytes:     64 << 20,
	Storage:      nats.MemoryStorage,
	BatchPublish: true,
	Replicas:     3,
}

var YouTubeIngressStream = StreamSpec{
	Name: "YOUTUBE_INGRESS",
	Subjects: []string{
		"youtube.ingress.event.premium",
		"youtube.ingress.event.standard",
		"youtube.ingress.event.stream",
		"youtube.ingress.status.>",
	},
	Retention:    nats.LimitsPolicy,
	MaxAge:       10 * time.Second,
	Storage:      nats.MemoryStorage,
	Replicas:     3,
	MaxBytes:     32 << 20,
	MaxMsgsPer:   100_000,
	Duplicates:   10 * time.Second,
	BatchPublish: true,
}

// TwitchIngressStream must precede TwitchIngressStandardStream, or the create is refused.
var DataStreams = []StreamSpec{
	BagelDataStream,
	TwitchIngressStream,
	TwitchIngressStandardStream,
	TwitchIngressRetryStream,
}

// Flip on only after every ingress publisher stages per-subject cohorts, or mixed batches drop.
func IngressPartitionEnabled() bool {
	return env.Get("NATS_INGRESS_PARTITION", "off") == "on"
}

func IngressLaneSpecs() []StreamSpec {
	if IngressPartitionEnabled() {
		return []StreamSpec{TwitchIngressStream, TwitchIngressStandardStream}
	}
	return []StreamSpec{legacyTwitchIngressStream()}
}

func legacyTwitchIngressStream() StreamSpec {
	spec := TwitchIngressStream
	spec.Subjects = []string{"twitch.ingress.event.>", "twitch.ingress.status.>"}
	spec.MaxBytes = 1 << 30
	return spec
}

type streamTopicResult struct {
	name string
	err  error
}

type streamTopicCache struct {
	partitioned bool
	entries     sync.Map
}

var streamTopicCachePtr atomic.Pointer[streamTopicCache]

func streamForTopic(topic string) (string, error) {
	partitioned := IngressPartitionEnabled()
	cache := streamTopicCachePtr.Load()
	for cache == nil || cache.partitioned != partitioned {
		next := &streamTopicCache{partitioned: partitioned}
		if streamTopicCachePtr.CompareAndSwap(cache, next) {
			cache = next
			break
		}
		cache = streamTopicCachePtr.Load()
	}
	if hit, ok := cache.entries.Load(topic); ok {
		res := hit.(streamTopicResult)
		return res.name, res.err
	}
	name, err := resolveStreamForTopic(topic)
	cache.entries.Store(topic, streamTopicResult{name: name, err: err})
	return name, err
}

func resolveStreamForTopic(topic string) (string, error) {
	specs := make([]StreamSpec, 0, len(DataStreams)+2)
	specs = append(specs, BagelDataStream)
	specs = append(specs, IngressLaneSpecs()...)
	specs = append(specs, TwitchIngressRetryStream, OutgressStream, OutgressSystemStream,
		YouTubeOutgressStream, YouTubeIngressStream,
		DiscordIngressStream, DiscordOutgressStream)

	for _, spec := range specs {
		if matchesAnySubject(topic, spec.Subjects) {
			return spec.Name, nil
		}
	}
	return "", fmt.Errorf("bus: no stream matches subject %q", topic)
}

func matchSubject(subject, filter string) bool {
	if strings.HasSuffix(filter, ">") {
		return strings.HasPrefix(subject, strings.TrimSuffix(filter, ">"))
	}
	return subject == filter
}

func matchesAnySubject(topic string, filters []string) bool {
	for _, filter := range filters {
		if matchSubject(topic, filter) {
			return true
		}
	}
	return false
}
