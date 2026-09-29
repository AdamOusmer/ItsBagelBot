import { describe, expect, test } from 'bun:test';
import { classifyFailure, failureKey, offlineFailure } from './errors';

describe('classifyFailure', () => {
  test('timeouts win over status', () => {
    expect(classifyFailure({ aborted: 'timeout', status: 502 })).toBe('timeout');
  });
  test('rate limit by code or status', () => {
    expect(classifyFailure({ code: 'rate_limited' })).toBe('rate_limited');
    expect(classifyFailure({ status: 429 })).toBe('rate_limited');
  });
  test('service unavailable by code or gateway status', () => {
    expect(classifyFailure({ code: 'unavailable' })).toBe('unavailable');
    expect(classifyFailure({ status: 502 })).toBe('unavailable');
    expect(classifyFailure({ status: 504 })).toBe('unavailable');
  });
  test('everything else is generic', () => {
    expect(classifyFailure({ status: 422 })).toBe('generic');
    expect(classifyFailure({})).toBe('generic');
  });
  test('offline is reported only without a connection', () => {
    expect(offlineFailure(false)).toBe('offline');
    expect(offlineFailure(true)).toBe('unavailable');
  });
  test('every failure has a catalog key', () => {
    expect(failureKey('offline')).toBe('import.errOffline');
    expect(failureKey('timeout')).toBe('import.errTimeout');
  });
});
