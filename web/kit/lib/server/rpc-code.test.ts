// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The refusal vocabulary exists TWICE, in two languages: Go's
// internal/domain/rpc/code.go declares it for the seven db services that emit
// it, and rpc-code.ts declares it for the console that branches on it.
//
// Nothing but this test ties them together, and a drift is silent in the worst
// way: rename a code in Go and the console keeps checking the old spelling, so
// every reply carrying the new one reads as "no code" and falls through to a
// generic failure -- the exact bug class the codes were added to remove.
//
// The test reads the Go source rather than a generated manifest, matching
// discord-permissions.test.ts and catalog/go-registry.test.ts, and rejects the
// same alternatives for the same reasons: a Go test reading .ts inverts the
// dependency, and a checked-in JSON artifact is one more thing that can go
// stale between the two.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { RPC_CODES, codeReader, rpcCode } from './rpc-code';

const GO_CODE = join(import.meta.dir, '../../../../internal/domain/rpc/code.go');

// goCodes reads the Codes() list rather than the const block: Codes() is what
// the Go side asserts against in its own test, so the two languages are pinned
// to the same declaration instead of to two spellings of it.
function goCodes(): string[] {
  const src = readFileSync(GO_CODE, 'utf8');
  const body = src.match(/func Codes\(\) \[\]Code \{\n\treturn \[\]Code\{([^}]+)\}/);
  if (!body) throw new Error('Codes() not found in code.go (declaration shape changed?)');
  const names = body[1].split(',').map((n) => n.trim()).filter(Boolean);
  return names.map((name) => {
    const decl = src.match(new RegExp(`^\\t${name} Code = "([a-z_]+)"$`, 'm'));
    if (!decl) throw new Error(`${name} has no string literal in code.go`);
    return decl[1];
  });
}

describe('rpc code vocabulary', () => {
  test('matches the Go declaration exactly', () => {
    expect([...RPC_CODES]).toEqual(goCodes());
  });
});

describe('codeReader', () => {
  const read = codeReader(['not_found', 'bound_elsewhere'] as const);

  test('reads the code field, ignoring the sentence', () => {
    expect(read({ code: 'not_found', error: 'anything at all' })).toBe('not_found');
  });

  test('drops a code the caller has no branch for', () => {
    expect(read({ code: 'conflict', error: 'stale' })).toBe('');
  });

  test('falls back to the legacy sentence only for codes the caller knows', () => {
    const legacy = { error: 'that server is already linked to another Twitch channel' };
    expect(read(legacy)).toBe('bound_elsewhere');
    expect(codeReader(['not_found'] as const)(legacy)).toBe('');
  });

  test('a success reply is not a refusal', () => {
    expect(read({})).toBe('');
    expect(rpcCode({ code: '', error: '' })).toBe('');
  });

  test('survives a reply whose fields are absent', () => {
    expect(read({ code: undefined, error: undefined })).toBe('');
  });
});

// The Go side sends `code` as an omitempty field, so a reply from a service
// that has not shipped it yet simply has no key. Proving the reader handles
// both shapes is the console half of the wire-compatibility gate; the Go half
// is TestWireCompatBothDirections in internal/domain/rpc/code_test.go.
describe('wire compatibility', () => {
  test('decodes a new reply and an old one the same way', () => {
    const fresh = JSON.parse('{"error":"no such user","code":"not_found"}');
    const old = JSON.parse('{"error":"no such user"}');
    expect(rpcCode(fresh)).toBe('not_found');
    expect(rpcCode(old)).toBe('');
    expect(fresh.error).toBe(old.error);
  });
});
