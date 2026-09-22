import { describe, expect, mock, test } from 'bun:test';
import { staticText } from '../../../../kit/lib/i18n/static';

mock.module('@bagel/kit', () => ({
  translate: (locale: 'en' | 'fr', key: string, params: Record<string, string | number> = {}) => {
    let value = staticText(locale, key);
    for (const [name, replacement] of Object.entries(params)) value = value.split(`{${name}}`).join(String(replacement));
    return value;
  }
}));
const { actionError } = await import('./action-errors');

describe('action error presentation', () => {
  test('both existing reply-length formats preserve their numeric limit in French', () => {
    for (const [message, max] of [
      ['Reply is too long (max 200 characters).', '200'],
      ['Reply is too long (max 500).', '500']
    ]) {
      const translated = actionError('fr', message!);
      expect(translated).not.toBe(message);
      expect(translated).toContain(max!);
      expect(translated).not.toContain('{max}');
    }
  });
  test('module refusals are localized without replacing unrelated diagnostics', () => {
    expect(actionError('fr', 'Invalid timer.')).toBe('Minuteur invalide.');
    const diagnostic = 'Custom source says: Invalid timer. https://example.com/?q=Gift';
    expect(actionError('fr', diagnostic)).toBe(diagnostic);
  });
});
