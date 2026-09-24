import { describe, expect, test } from 'bun:test';
import { compareUses, usesCount } from './uses';
import { formatCounterValue } from './validation';

describe('command uses', () => {
  test('keeps every digit at the signed int64 ceiling', () => {
    const value = usesCount({ uses: '9223372036854775807' });
    expect(value).toBe(9223372036854775807n);
    expect(formatCounterValue(value.toString(), 'en-US')).toBe('9,223,372,036,854,775,807');
  });
  test('sorts adjacent large values and sums them exactly', () => {
    const rows = [{ uses: '9223372036854775806' }, { uses: '9223372036854775807' }];
    expect([...rows].sort((a, b) => compareUses(b, a))[0].uses).toBe('9223372036854775807');
    expect(rows.reduce((n, row) => n + usesCount(row), 0n).toString()).toBe('18446744073709551613');
  });
  test('defaults only missing values and rejects corrupt values', () => {
    expect(usesCount({})).toBe(0n);
    for (const uses of ['-1', '1.5', '9223372036854775808', '1.2k']) {
      expect(() => usesCount({ uses })).toThrow();
    }
  });
});
