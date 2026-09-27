import { describe, expect, test } from 'bun:test';
import { formatPointValue, parsePointValue, readPointBalance } from './point-value';

describe('point balances', () => {
  test('preserves exact balances above the browser integer range through JSON', () => {
    for (const points of ['9007199254740993', '9223372036854775807', '-9223372036854775808']) {
      const wire = JSON.parse(JSON.stringify({ points: Number(points), points_exact: points }));
      expect(readPointBalance(wire)).toBe(points);
    }
    expect(formatPointValue('9007199254740993', 'en-US')).toBe('9,007,199,254,740,993');
  });

  test('accepts safe legacy numbers and normalizes decimal values', () => {
    for (const points of [0, -1, Number.MAX_SAFE_INTEGER]) {
      expect(readPointBalance({ points })).toBe(String(points));
    }
    expect(parsePointValue('00042')).toBe('42');
    expect(parsePointValue('-0')).toBe('0');
  });

  test('rejects malformed and out-of-range exact fields without numeric fallback', () => {
    for (const points_exact of ['', '1.5', '1e3', '+1', '9223372036854775808', '-9223372036854775809', null]) {
      expect(() => readPointBalance({ points: 42, points_exact })).toThrow('invalid point balance');
    }
  });

  test('rejects rounded and nonnumeric legacy balances', () => {
    for (const points of [Number.MAX_SAFE_INTEGER + 1, 0.5, Infinity, '42', null, undefined]) {
      expect(() => readPointBalance({ points })).toThrow('invalid point balance');
    }
  });
});
