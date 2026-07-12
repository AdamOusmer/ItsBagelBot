import { describe, expect, test } from 'bun:test';
import { positiveIntegerSetting } from './config-sanity';

describe('positiveIntegerSetting', () => {
  test('uses the documented fallback when the setting is absent', () => {
    expect(positiveIntegerSetting('CACHE_CAPACITY', undefined, 250)).toBe(250);
  });

  test('accepts a configured positive integer', () => {
    expect(positiveIntegerSetting('CACHE_CAPACITY', '1000', 250)).toBe(1000);
  });

  test.each(['', '0', '-1', '1.5', '1e3', 'entries'])(
    'rejects invalid capacity %p',
    (value) => {
      expect(() => positiveIntegerSetting('CACHE_CAPACITY', value, 250)).toThrow(
        'CACHE_CAPACITY must be a positive integer'
      );
    }
  );

  test('rejects integers outside the safe range', () => {
    expect(() =>
      positiveIntegerSetting('CACHE_CAPACITY', '9007199254740992', 250)
    ).toThrow('CACHE_CAPACITY must be a safe integer');
  });
});
