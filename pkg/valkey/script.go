// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"crypto/sha1"
	"encoding/hex"

	valkey_go "github.com/valkey-io/valkey-go"
)

type Script struct {
	body string
	sha  string
}

func NewScript(body string) Script {
	sum := sha1.Sum([]byte(body))
	return Script{body: body, sha: hex.EncodeToString(sum[:])}
}

func (s Script) Body() string { return s.body }

func (s Script) SHA1() string { return s.sha }

type ScriptCall struct {
	Script Script
	Key    string
	Args   []string
}

func (c ScriptCall) evalsha(client valkey_go.Client) valkey_go.Completed {
	return client.B().Evalsha().Sha1(c.Script.sha).Numkeys(1).Key(c.Key).Arg(c.Args...).Build()
}

func (c ScriptCall) eval(client valkey_go.Client) valkey_go.Completed {
	return client.B().Eval().Script(c.Script.body).Numkeys(1).Key(c.Key).Arg(c.Args...).Build()
}

// RunScripts pipelines every call as EVALSHA and resends only the NOSCRIPT misses as EVAL.
func RunScripts(ctx context.Context, client valkey_go.Client, calls []ScriptCall) []valkey_go.ValkeyResult {
	commands := make([]valkey_go.Completed, len(calls))
	for i, call := range calls {
		commands[i] = call.evalsha(client)
	}
	results := client.DoMulti(ctx, commands...)
	missing := missingScripts(results)
	if len(missing) == 0 {
		return results
	}
	reload := make([]valkey_go.Completed, len(missing))
	for i, index := range missing {
		reload[i] = calls[index].eval(client)
	}
	for i, result := range client.DoMulti(ctx, reload...) {
		results[missing[i]] = result
	}
	return results
}

func missingScripts(results []valkey_go.ValkeyResult) []int {
	var missing []int
	for i, result := range results {
		if err, ok := valkey_go.IsValkeyErr(result.Error()); ok && err.IsNoScript() {
			missing = append(missing, i)
		}
	}
	return missing
}
