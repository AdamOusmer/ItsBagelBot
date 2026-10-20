// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const SERVER_ERROR_KEYS: Record<string, string> = {
  'Choose a file to upload.': 'errFileMissing',
  'Paste your StreamElements JWT first.': 'errJwtMissing',
  'Enter your Fossabot channel name first.': 'errFossabotMissing',
  'That does not look like a Fossabot channel name.': 'errFossabotShape',
  'Enter your Wizebot channel name first.': 'errWizebotMissing',
  'That does not look like a Wizebot channel name.': 'errWizebotShape',
  'That does not look like a StreamElements JWT. Copy the whole token: three segments separated by dots, no spaces.':
    'errJwtShape'
};

export function localizeImporterError(
  message: string | undefined,
  translate: (key: string) => string
): string {
  if (!message) return '';
  const key = SERVER_ERROR_KEYS[message];
  return key ? translate(key) : message;
}
