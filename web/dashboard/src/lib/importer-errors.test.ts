import { expect, test } from 'bun:test';
import { localizeImporterError } from './importer-errors';

const translate = (key: string) => `translated:${key}`;

const cases = [
  { name: 'maps finite server refusals through the active catalog', message: 'That does not look like a Wizebot channel name.', want: 'translated:import.errWizebotShape' },
  { name: 'leaves dynamic parser or upstream diagnostics intact', message: 'Fossabot returned an unexpected response.', want: 'Fossabot returned an unexpected response.' },
  { name: 'an absent message is empty', message: undefined, want: '' }
];

test.each(cases)('localizeImporterError $name', ({ message, want }) => {
  expect(localizeImporterError(message, translate)).toBe(want);
});
