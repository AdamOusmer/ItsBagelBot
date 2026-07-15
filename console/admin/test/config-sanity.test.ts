// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
import { assertDemoConfigSafe } from '../src/lib/server/config-sanity';

describe('admin demo configuration', () => {
  test('rejects DEMO in production', () => {
    expect(() => assertDemoConfigSafe({ DEMO: '1', NODE_ENV: 'production' })).toThrow(
      'DEMO must not be enabled in production'
    );
  });

  test('allows DEMO only outside production', () => {
    expect(() => assertDemoConfigSafe({ DEMO: '1', NODE_ENV: 'development' })).not.toThrow();
    expect(() => assertDemoConfigSafe({ NODE_ENV: 'production' })).not.toThrow();
  });
});
