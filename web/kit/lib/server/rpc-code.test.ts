// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { RPC_CODES, codeReader, rpcCode } from './rpc-code';

const GO_CODE = join(import.meta.dir, '../../../../internal/domain/rpc/code.go');

function goCodes(): string[] {
  const src = readFileSync(GO_CODE, 'utf8');
  const body = src.match(/func Codes\(\) \[\]Code \{\n\treturn \[\]Code\{([^}]+)\}/);
  if (!body) throw new Error('Codes() not found in code.go (declaration shape changed?)');
  const names = body[1].split(',').map((n) => n.trim()).filter(Boolean);
  return names.map((name) => {
    const decl = src.match(new RegExp(`^\\t${name}\\s+Code\\s+=\\s+"([a-z_]+)"$`, 'm'));
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

describe('wire compatibility', () => {
  test('decodes a new reply and an old one the same way', () => {
    const fresh = JSON.parse('{"error":"no such user","code":"not_found"}');
    const old = JSON.parse('{"error":"no such user"}');
    expect(rpcCode(fresh)).toBe('not_found');
    expect(rpcCode(old)).toBe('');
    expect(fresh.error).toBe(old.error);
  });
});
