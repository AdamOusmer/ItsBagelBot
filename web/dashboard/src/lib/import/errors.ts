// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type ImportFailure = 'offline' | 'timeout' | 'rate_limited' | 'unavailable' | 'generic';

export const SERVER_ERROR_CODES = ['rate_limited', 'unavailable'] as const;

const FAILURE_KEYS: Record<ImportFailure, string> = {
  offline: 'import.errOffline',
  timeout: 'import.errTimeout',
  rate_limited: 'import.errRateLimited',
  unavailable: 'import.errUnavailable',
  generic: 'import.errGeneric'
};

export function failureKey(kind: ImportFailure): string {
  return FAILURE_KEYS[kind];
}

type FailureInput = { status?: number; code?: string; aborted?: 'timeout' | 'cancel' };

const UNAVAILABLE_STATUSES = new Set([502, 503, 504]);

const isRateLimited = (input: FailureInput) => input.code === 'rate_limited' || input.status === 429;

const isUnavailable = (input: FailureInput) =>
  input.code === 'unavailable' || UNAVAILABLE_STATUSES.has(input.status ?? 0);

export function classifyFailure(input: FailureInput): ImportFailure {
  if (input.aborted === 'timeout') return 'timeout';
  if (isRateLimited(input)) return 'rate_limited';
  if (isUnavailable(input)) return 'unavailable';
  return 'generic';
}

export function offlineFailure(online: boolean): ImportFailure {
  return online ? 'unavailable' : 'offline';
}
