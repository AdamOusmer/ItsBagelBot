import { describe, expect, it } from 'bun:test';
import { fmtDateTime } from './format';

describe('fmtDateTime', () => {
  it('formats a timestamp with its timezone without invalid Intl option combinations', () => {
    const formatted = fmtDateTime('2026-09-15T12:34:00Z');
    expect(formatted).toContain('2026');
    expect(formatted).not.toBe('Invalid Date');
  });

  it('uses the supplied fallback for absent or invalid timestamps', () => {
    expect(fmtDateTime(null, 'Date not verified')).toBe('Date not verified');
    expect(fmtDateTime('bad', 'Date not verified')).toBe('Date not verified');
  });
});
