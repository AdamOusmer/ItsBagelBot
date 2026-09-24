// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export * from './fetch-tokens';

export const FETCH_NAME_MAX = 32;
export const FETCH_URL_MAX = 512;
export const JSON_PATH_MAX_DEPTH = 8;
export const URLFETCH_TOKEN_CAP = 3;
export const KEY_LABEL_MAX = 32;
export const KEY_VALUE_MAX = 512;
export const DEFS_PER_BROADCASTER = 20;

const FETCH_NAME_RE = /^[a-z0-9_]+$/;
export const PATH_SEGMENT_RE = /^[A-Za-z0-9_-]+$/;

export type FetchKind = 'plain' | 'json';

export interface FetchDefFields {
  name: string;
  url: string;
  kind: FetchKind;
  path: string[];
  keyLabel: string;
}

export type FetchDefErrors = Partial<Record<'name' | 'url' | 'kind' | 'path' | 'key_label', string>>;

function hostIsDenied(host: string): boolean {
  const bare = host.replace(/^\[/, '').replace(/\]$/, '');
  return deniedName(bare) || ipLiteral(bare);
}

const DENIED_HOSTS = new Set(['localhost']);
const DENIED_SUFFIXES = ['.local', '.internal'];
const IPV4_LITERAL_RE = /^(\d{1,3}\.){3}\d{1,3}$/;

function deniedName(bare: string): boolean {
  if (DENIED_HOSTS.has(bare)) return true;
  return DENIED_SUFFIXES.some((suffix) => bare.endsWith(suffix));
}

function ipLiteral(bare: string): boolean {
  if (bare.includes(':')) return true;
  return IPV4_LITERAL_RE.test(bare);
}

function fetchNameProblem(f: FetchDefFields): string | undefined {
  if (!f.name) return 'Definition name is required.';
  if (f.name.length > FETCH_NAME_MAX) return `Definition name must be at most ${FETCH_NAME_MAX} characters.`;
  if (!FETCH_NAME_RE.test(f.name)) return 'Use lower-case letters, digits and underscores only.';
  return undefined;
}

function parsedHttpsUrl(url: string): URL | null {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }
  if (parsed.protocol !== 'https:') return null;
  if (!parsed.hostname) return null;
  return parsed;
}

function fetchUrlProblem(f: FetchDefFields): string | undefined {
  if (!f.url) return 'URL is required.';
  if (f.url.length > FETCH_URL_MAX) return `URL must be at most ${FETCH_URL_MAX} characters.`;
  const parsed = parsedHttpsUrl(f.url);
  if (!parsed) return 'URL must start with https://';
  if (hostIsDenied(parsed.hostname)) return 'URL must point at a public https host.';
  return undefined;
}

function kindPathProblem(f: FetchDefFields): { field: 'kind' | 'path'; msg: string } | undefined {
  if (f.kind !== 'plain' && f.kind !== 'json') return { field: 'kind', msg: 'Pick plain or json.' };
  if (f.kind === 'plain') {
    if (f.path.length === 0) return undefined;
    return { field: 'path', msg: 'A plain fetch reads the whole body. Clear the path or switch to json.' };
  }
  return jsonPathProblem(f);
}

function jsonPathProblem(f: FetchDefFields): { field: 'path'; msg: string } | undefined {
  if (f.path.length > JSON_PATH_MAX_DEPTH) {
    return { field: 'path', msg: `Path can be at most ${JSON_PATH_MAX_DEPTH} segments deep.` };
  }
  const bad = f.path.find((s) => !PATH_SEGMENT_RE.test(s));
  if (bad === undefined) return undefined;
  return { field: 'path', msg: `"${bad}" cannot be used as a path segment: letters, digits, "-" and "_" only.` };
}

export function validateFetchDef(f: FetchDefFields): FetchDefErrors {
  const errors: FetchDefErrors = {};
  const name = fetchNameProblem(f);
  if (name !== undefined) errors.name = name;
  const url = fetchUrlProblem(f);
  if (url !== undefined) errors.url = url;
  const kindPath = kindPathProblem(f);
  if (kindPath !== undefined) errors[kindPath.field] = kindPath.msg;
  if (f.keyLabel.length > KEY_LABEL_MAX) errors.key_label = `Key label must be at most ${KEY_LABEL_MAX} characters.`;
  return errors;
}

