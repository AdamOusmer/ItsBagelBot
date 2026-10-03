import { expect, test } from 'bun:test';
import { classifyFailure, failureKey, offlineFailure, type ImportFailure } from './errors';

const failures: { name: string; input: Parameters<typeof classifyFailure>[0]; kind: ImportFailure; key: string }[] = [
  { name: 'timeouts win over status', input: { aborted: 'timeout', status: 502 }, kind: 'timeout', key: 'import.errTimeout' },
  { name: 'rate limit by code', input: { code: 'rate_limited' }, kind: 'rate_limited', key: 'import.errRateLimited' },
  { name: 'rate limit by status', input: { status: 429 }, kind: 'rate_limited', key: 'import.errRateLimited' },
  { name: 'service unavailable by code', input: { code: 'unavailable' }, kind: 'unavailable', key: 'import.errUnavailable' },
  { name: 'service unavailable by 502', input: { status: 502 }, kind: 'unavailable', key: 'import.errUnavailable' },
  { name: 'service unavailable by 504', input: { status: 504 }, kind: 'unavailable', key: 'import.errUnavailable' },
  { name: 'an unrecognised status is generic', input: { status: 422 }, kind: 'generic', key: 'import.errGeneric' },
  { name: 'an empty failure is generic', input: {}, kind: 'generic', key: 'import.errGeneric' }
];

test.each(failures)('classifyFailure $name', ({ input, kind, key }) => {
  const failure = classifyFailure(input);
  expect([failure, failureKey(failure)]).toEqual([kind, key]);
});

test.each([
  { online: false, kind: 'offline', key: 'import.errOffline' },
  { online: true, kind: 'unavailable', key: 'import.errUnavailable' }
])('offlineFailure is $kind when online is $online', ({ online, kind, key }) => {
  const failure = offlineFailure(online);
  expect([failure, failureKey(failure)]).toEqual([kind, key]);
});
