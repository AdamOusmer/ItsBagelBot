import { describe, expect, test } from 'bun:test';
import { localizeImporterError } from './importer-errors';

describe('localizeImporterError', () => {
  test('maps finite server refusals through the active catalog', () => {
    const seen: string[] = [];
    const translate = (key: string) => {
      seen.push(key);
      return `translated:${key}`;
    };

    expect(localizeImporterError('That does not look like a Wizebot channel name.', translate)).toBe(
      'translated:import.errWizebotShape'
    );
    expect(seen).toEqual(['import.errWizebotShape']);
  });

  test('leaves dynamic parser or upstream diagnostics intact', () => {
    const translate = () => {
      throw new Error('dynamic diagnostics must not be looked up');
    };
    expect(localizeImporterError('Fossabot returned an unexpected response.', translate)).toBe(
      'Fossabot returned an unexpected response.'
    );
    expect(localizeImporterError(undefined, translate)).toBe('');
  });
});
