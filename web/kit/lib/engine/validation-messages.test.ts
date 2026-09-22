// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, it } from 'bun:test';
import { validateFetchDef } from './fetch-validate';
import { translateValidationMessage } from './validation-messages';

const fr = (key: string, params?: Record<string, string | number>) =>
  ({
    'validation.commandNameRequired': 'Le nom de commande est obligatoire.',
    'validation.commandNameMax': `Le nom de commande doit compter au plus ${params?.max} caractères.`,
    'validation.urlPublic': 'L’URL doit viser un hôte https public.',
    'validation.urlHttps': 'L’URL doit commencer par https://',
    'validation.pathSegment': `« ${params?.segment} » ne peut pas être utilisé comme segment de chemin.`
  })[key] ?? key;

describe('translateValidationMessage', () => {
  it('translates stable command and fetch validation messages', () => {
    expect(translateValidationMessage('Command name is required.', fr)).toBe('Le nom de commande est obligatoire.');
    expect(translateValidationMessage('Command name must be at most 64 characters.', fr)).toContain('64');
    expect(translateValidationMessage('URL must start with https://', fr)).toBe('L’URL doit commencer par https://');
  });

  it('localizes actual public-host and path validator failures', () => {
    const errors = validateFetchDef({ name: 'weather', url: 'https://localhost', kind: 'json', path: ['my custom field'], keyLabel: '' });
    expect(translateValidationMessage(errors.url, fr)).toBe('L’URL doit viser un hôte https public.');
    expect(translateValidationMessage(errors.path, fr)).toContain('my custom field');
  });

  it('preserves user supplied names and unknown messages', () => {
    expect(translateValidationMessage('"city-temp" cannot be used as a path segment: letters, digits, "-" and "_" only.', fr)).toContain('city-temp');
    expect(translateValidationMessage('A future validator message.', fr)).toBe('A future validator message.');
  });
});
