import { expect, test } from 'bun:test';
import { classifyFailure, failureKey, offlineFailure, type ImportFailure } from './errors';

const failures: { name: string; input: Parameters<typeof classifyFailure>[0]; kind: ImportFailure; key: string }[] = [
  { name: 'timeouts win over status', input: { aborted: 'timeout', status: 502 }, kind: 'timeout', key: 'errTimeout' },
  { name: 'rate limit by code', input: { code: 'rate_limited' }, kind: 'rate_limited', key: 'errRateLimited' },
  { name: 'rate limit by status', input: { status: 429 }, kind: 'rate_limited', key: 'errRateLimited' },
  { name: 'service unavailable by code', input: { code: 'unavailable' }, kind: 'unavailable', key: 'errUnavailable' },
  { name: 'service unavailable by 502', input: { status: 502 }, kind: 'unavailable', key: 'errUnavailable' },
  { name: 'service unavailable by 504', input: { status: 504 }, kind: 'unavailable', key: 'errUnavailable' },
  { name: 'an unrecognised status is generic', input: { status: 422 }, kind: 'generic', key: 'errGeneric' },
  { name: 'an empty failure is generic', input: {}, kind: 'generic', key: 'errGeneric' }
];

test.each(failures)('classifyFailure $name', ({ input, kind, key }) => {
  const failure = classifyFailure(input);
  expect([failure, failureKey(failure)]).toEqual([kind, key]);
});

test.each([
  { online: false, kind: 'offline', key: 'errOffline' },
  { online: true, kind: 'unavailable', key: 'errUnavailable' }
])('offlineFailure is $kind when online is $online', ({ online, kind, key }) => {
  const failure = offlineFailure(online);
  expect([failure, failureKey(failure)]).toEqual([kind, key]);
});
