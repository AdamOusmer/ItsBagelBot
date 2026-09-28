import { describe, expect, test } from 'bun:test';
import { formatInt64, MAX_INT64, MIN_INT64, parseInt64 } from './int64-value';
import { formatCounterValue, formatPointValue, parseCounterValue, parsePointValue } from './validation';

const NNBSP = ' ';

describe('signed BIGINT values', () => {
  test('parse keeps both bounds exact and rejects one past them', () => {
    expect(parseInt64(MAX_INT64.toString())).toBe('9223372036854775807');
    expect(parseInt64(MIN_INT64.toString())).toBe('-9223372036854775808');
    expect(parseInt64('-9223372036854775807')).toBe('-9223372036854775807');
    expect(parseInt64('9223372036854775808')).toBeNull();
    expect(parseInt64('-9223372036854775809')).toBeNull();
  });

  test('a zero minimum is the unsigned counter range', () => {
    expect(parseInt64('0', 0n)).toBe('0');
    expect(parseInt64('-0', 0n)).toBeNull();
    expect(parseInt64('-1', 0n)).toBeNull();
    expect(parseInt64(-1, 0n)).toBeNull();
    expect(parseInt64(MAX_INT64.toString(), 0n)).toBe('9223372036854775807');
  });

  test('points and counters share one parser at their own lower bounds', () => {
    expect(parsePointValue('-42')).toBe('-42');
    expect(parseCounterValue('-42')).toBeNull();
    expect(parsePointValue(' 0042 ')).toBe(parseCounterValue(' 0042 '));
  });

  test.each([
    ['9223372036854775807', '9,223,372,036,854,775,807', `9${NNBSP}223${NNBSP}372${NNBSP}036${NNBSP}854${NNBSP}775${NNBSP}807`],
    ['-9223372036854775807', '-9,223,372,036,854,775,807', `-9${NNBSP}223${NNBSP}372${NNBSP}036${NNBSP}854${NNBSP}775${NNBSP}807`],
    ['-9223372036854775808', '-9,223,372,036,854,775,808', `-9${NNBSP}223${NNBSP}372${NNBSP}036${NNBSP}854${NNBSP}775${NNBSP}808`],
    ['0', '0', '0'],
    ['-1234', '-1,234', `-1${NNBSP}234`],
    ['-7', '-7', '-7'],
  ])('formats %s exactly in English and French', (raw, en, fr) => {
    expect(formatInt64(raw, 'en')).toBe(en);
    expect(formatInt64(raw, 'fr')).toBe(fr);
    expect(formatPointValue(raw, 'en')).toBe(en);
    expect(formatPointValue(raw, 'fr')).toBe(fr);
  });

  test('counter and point formatting are the same function', () => {
    expect(formatCounterValue).toBe(formatInt64);
    expect(formatPointValue).toBe(formatInt64);
  });
});
