// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type DecodeError = 'unknown_field' | 'mismatch';

export type Kind =
  | 'string'
  | 'int'
  | 'bool'
  | 'strings'
  | 'strmap'
  | 'ptrbool'
  | 'ptrint'
  | 'raw'
  | 'typed'
  | 'typeds'
  | 'actions'
  | 'branches';

export type Schema = Record<string, Kind>;

interface Ctx {
  err?: DecodeError;
}

const INT64_LIMIT = 2 ** 63;

export const TYPED_SCHEMA: Schema = { type: 'string', params: 'raw' };
export const ACTION_SCHEMA: Schema = { type: 'string', enabled: 'ptrbool', params: 'raw' };
export const BRANCH_SCHEMA: Schema = { weight: 'int', actions: 'actions' };

function fail(ctx: Ctx, err: DecodeError): void {
  ctx.err ??= err;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function decodeString(v: unknown, ctx: Ctx): string {
  if (typeof v === 'string') return v;
  fail(ctx, 'mismatch');
  return '';
}

function orEmpty(e: unknown, ctx: Ctx): string {
  return e === null ? '' : decodeString(e, ctx);
}

function fitsInt64(n: number): boolean {
  return Number.isInteger(n) && Math.abs(n) < INT64_LIMIT;
}

function decodeInt(v: unknown, ctx: Ctx): number {
  if (typeof v === 'number' && fitsInt64(v)) return v;
  fail(ctx, 'mismatch');
  return 0;
}

function decodeBool(v: unknown, ctx: Ctx): boolean {
  if (typeof v === 'boolean') return v;
  fail(ctx, 'mismatch');
  return false;
}

function decodeList<T>(v: unknown, ctx: Ctx, item: (e: unknown) => T): T[] {
  if (Array.isArray(v)) return v.map(item);
  fail(ctx, 'mismatch');
  return [];
}

function decodeMap(v: unknown, ctx: Ctx): Record<string, string> {
  const out: Record<string, string> = {};
  if (!isObject(v)) {
    fail(ctx, 'mismatch');
    return out;
  }
  for (const [key, value] of Object.entries(v)) {
    out[key] = orEmpty(value, ctx);
  }
  return out;
}

const ZERO_VALUES: Record<Kind, () => unknown> = {
  string: () => '',
  int: () => 0,
  bool: () => false,
  strings: () => [],
  typeds: () => [],
  actions: () => [],
  branches: () => [],
  strmap: () => ({}),
  ptrbool: () => null,
  ptrint: () => null,
  raw: () => undefined,
  typed: () => ({ type: '', params: undefined }),
};

function zeroOf(kind: Kind): unknown {
  return ZERO_VALUES[kind]();
}

const VALUE_DECODERS: Record<Kind, (v: unknown, ctx: Ctx) => unknown> = {
  string: decodeString,
  int: decodeInt,
  bool: decodeBool,
  strings: (v, ctx) => decodeList(v, ctx, (e) => orEmpty(e, ctx)),
  strmap: decodeMap,
  ptrbool: decodeBool,
  ptrint: decodeInt,
  raw: (v) => v,
  typed: (v, ctx) => decodeStruct(v, TYPED_SCHEMA, {}, ctx),
  typeds: (v, ctx) => decodeList(v, ctx, (e) => decodeStruct(e, TYPED_SCHEMA, {}, ctx)),
  actions: (v, ctx) => decodeList(v, ctx, (e) => decodeStruct(e, ACTION_SCHEMA, {}, ctx)),
  branches: (v, ctx) => decodeList(v, ctx, (e) => decodeStruct(e, BRANCH_SCHEMA, {}, ctx)),
};

function resolveField(schema: Schema, key: string): string | undefined {
  if (Object.hasOwn(schema, key)) return key;
  const folded = key.toLowerCase();
  return Object.keys(schema).find((name) => name.toLowerCase() === folded);
}

function zeroStruct(schema: Schema, defaults: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [name, kind] of Object.entries(schema)) out[name] = name in defaults ? defaults[name] : zeroOf(kind);
  return out;
}

function decodeFields(fields: Record<string, unknown>, schema: Schema, out: Record<string, unknown>, ctx: Ctx): void {
  for (const [key, value] of Object.entries(fields)) {
    const name = resolveField(schema, key);
    if (name === undefined) fail(ctx, 'unknown_field');
    else if (value !== null) out[name] = VALUE_DECODERS[schema[name]](value, ctx);
  }
}

function decodeStruct(v: unknown, schema: Schema, defaults: Record<string, unknown>, ctx: Ctx): Record<string, unknown> {
  const out = zeroStruct(schema, defaults);
  if (v === null || v === undefined) return out;
  if (isObject(v)) decodeFields(v, schema, out, ctx);
  else fail(ctx, 'mismatch');
  return out;
}

export function decodeStrict(v: unknown, schema: Schema, defaults: Record<string, unknown> = {}): { value: Record<string, unknown>; err?: DecodeError } {
  const ctx: Ctx = {};
  const value = decodeStruct(v, schema, defaults, ctx);
  return { value, err: ctx.err };
}
