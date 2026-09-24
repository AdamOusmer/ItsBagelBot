import { describe, expect, test } from 'bun:test';
import { isCounterValue, parseCounterValue } from './validation';

describe('counter values', () => {
  test('accepts exact nonnegative integers', () => {
    expect(parseCounterValue('0')).toBe('0');
    expect(parseCounterValue(String(Number.MAX_SAFE_INTEGER))).toBe(String(Number.MAX_SAFE_INTEGER));
    expect(parseCounterValue('9223372036854775807')).toBe('9223372036854775807');
  });

  test('rejects negative, fractional, and imprecise values', () => {
    for (const raw of ['', '-1', '+1', '1.5', '1e3']) {
      expect(parseCounterValue(raw)).toBeNull();
    }
    for (const raw of [-1, 0.5, Number.MAX_SAFE_INTEGER + 1, Infinity]) {
      expect(isCounterValue(raw)).toBe(false);
    }
    expect(parseCounterValue('9223372036854775808')).toBeNull();
  });
});
