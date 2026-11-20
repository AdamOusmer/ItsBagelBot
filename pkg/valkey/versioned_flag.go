// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"errors"

	valkey_go "github.com/valkey-io/valkey-go"
)

// VersionedFlag pairs a boolean flag key with a version counter, so every change can be told apart from every other.
type VersionedFlag struct {
	Flag    string
	Version string
}

// Set runs a MULTI/EXEC transaction on a dedicated connection: writes the flag (SET "1" when on, DEL when off) and increments Version, returning the new version.
func (f VersionedFlag) Set(ctx context.Context, client valkey_go.Client, on bool) (int64, error) {
	var version int64
	err := client.Dedicated(func(dc valkey_go.DedicatedClient) error {
		var txErr error
		version, txErr = f.runTxn(ctx, dc, on)
		return txErr
	})
	return version, err
}

func (f VersionedFlag) runTxn(ctx context.Context, client valkey_go.DedicatedClient, on bool) (int64, error) {
	stateCommand := client.B().Del().Key(f.Flag).Build()
	if on {
		stateCommand = client.B().Set().Key(f.Flag).Value("1").Build()
	}
	results := client.DoMulti(ctx,
		client.B().Multi().Build(),
		stateCommand,
		client.B().Incr().Key(f.Version).Build(),
		client.B().Exec().Build(),
	)
	executed, err := versionedFlagTxnResults(results)
	if err != nil {
		return 0, err
	}
	return executed[1].AsInt64()
}

func versionedFlagTxnResults(results []valkey_go.ValkeyResult) ([]valkey_go.ValkeyMessage, error) {
	if len(results) != 4 {
		return nil, errors.New("versioned flag transaction returned an invalid pipeline result")
	}
	if err := firstError(results[:len(results)-1]); err != nil {
		return nil, err
	}
	executed, err := results[len(results)-1].ToArray()
	if err != nil {
		return nil, err
	}
	if len(executed) != 2 {
		return nil, errors.New("versioned flag transaction returned an invalid result")
	}
	if err := firstMessageError(executed); err != nil {
		return nil, err
	}
	return executed, nil
}

func firstMessageError(messages []valkey_go.ValkeyMessage) error {
	for _, message := range messages {
		if err := message.Error(); err != nil {
			return err
		}
	}
	return nil
}

// Read returns the flag and version in one round trip.
func (f VersionedFlag) Read(ctx context.Context, client valkey_go.Client) (bool, int64, error) {
	flag, version, err := (VersionedSlot{Key: f.Flag, VerKey: f.Version}).Read(ctx, client)
	if err != nil {
		return false, 0, err
	}
	return flag == "1", version, nil
}
