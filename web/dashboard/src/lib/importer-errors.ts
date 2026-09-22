// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The importer server runs before a locale-aware component exists, so its
 * finite set of input refusals remains stable English prose on the wire.
 * Translate those known refusals at the page boundary; parser and upstream
 * diagnostics are dynamic and pass through unchanged.
 */
const SERVER_ERROR_KEYS: Record<string, string> = {
  'Choose a file to upload.': 'import.errFileMissing',
  'Paste your StreamElements JWT first.': 'import.errJwtMissing',
  'Enter your Fossabot channel name first.': 'import.errFossabotMissing',
  'That does not look like a Fossabot channel name.': 'import.errFossabotShape',
  'Enter your Wizebot channel name first.': 'import.errWizebotMissing',
  'That does not look like a Wizebot channel name.': 'import.errWizebotShape',
  'That does not look like a StreamElements JWT. Copy the whole token: three segments separated by dots, no spaces.':
    'import.errJwtShape'
};

export function localizeImporterError(
  message: string | undefined,
  translate: (key: string) => string
): string {
  if (!message) return '';
  const key = SERVER_ERROR_KEYS[message];
  return key ? translate(key) : message;
}
