// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { MessageKey } from '../i18n/keys';
import { parseMoobot } from './moobot';
import type { ImportDiagnostic, ImportManifest, ImportSource } from './types';

export interface ParseResult {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
}

export interface TextInputSpec {
  kind: 'text';
  secret: boolean;
  shape: RegExp;
  maxLen: number;
  placeholder: string;
  linkHref?: string;
  linkLabel?: MessageKey;
  i18n: { field: MessageKey; hint?: MessageKey; errMissing: MessageKey; errShape: MessageKey };
}

export interface FileInputSpec {
  kind: 'file';
  accept: string;
  maxBytes: number;
  parseInBrowser?: (bytes: Uint8Array) => ParseResult;
}

export interface OAuthInputSpec {
  kind: 'oauth';
  connectPath: string;
  i18n: { cta: MessageKey; connected: MessageKey; scopeHint: MessageKey; errNotConnected: MessageKey };
  errorParams: Record<string, MessageKey>;
}

export type InputSpec = TextInputSpec | FileInputSpec | OAuthInputSpec;

export type ChipKind = 'token' | 'file' | 'connect' | 'handle';

export const CHIP_LABEL_KEYS: Record<ChipKind, MessageKey> = {
  token: 'import.chipToken',
  file: 'import.chipFile',
  connect: 'import.chipConnect',
  handle: 'import.chipHandle'
};

export interface ImportSourceStrategy {
  id: ImportSource;
  label: string;
  initials: string;
  chip: ChipKind;
  available: boolean;
  i18n: { desc: MessageKey; instr?: MessageKey };
  input: InputSpec;
}

const JWT_SHAPE = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;
const MAX_JWT_LEN = 4096;

export const FOSSABOT_HANDLE_SHAPE = /^[A-Za-z0-9_.-]{1,64}$/;
export const MAX_HANDLE_LEN = 64;

const WIZEBOT_HANDLE_SHAPE = /^[A-Za-z0-9][A-Za-z0-9-]{0,24}$/;
const MAX_WIZEBOT_HANDLE_LEN = 25;

const MAX_MOOBOT_BYTES = 10 * 1024 * 1024;

const MAX_STREAMLABS_BYTES = 20 * 1024 * 1024;

export const IMPORT_STRATEGIES: Record<ImportSource, ImportSourceStrategy> = {
  streamelements: {
    id: 'streamelements',
    label: 'StreamElements',
    initials: 'SE',
    chip: 'token',
    available: true,
    i18n: { desc: 'import.seDesc', instr: 'import.instrSe' },
    input: {
      kind: 'text',
      secret: true,
      shape: JWT_SHAPE,
      maxLen: MAX_JWT_LEN,
      placeholder: 'eyJhbGciOi…',
      linkHref: 'https://streamelements.com/dashboard/account/channels',
      linkLabel: 'import.seLinkLabel',
      i18n: {
        field: 'import.jwtFieldAria',
        hint: 'import.jwtHint',
        errMissing: 'import.errJwtMissing',
        errShape: 'import.errJwtMissing'
      }
    }
  },
  fossabot: {
    id: 'fossabot',
    label: 'Fossabot',
    initials: 'F',
    chip: 'handle',
    available: true,
    i18n: { desc: 'import.fossabotDesc', instr: 'import.instrFossabot' },
    input: {
      kind: 'text',
      secret: false,
      shape: FOSSABOT_HANDLE_SHAPE,
      maxLen: MAX_HANDLE_LEN,
      placeholder: 'yourchannel',
      i18n: {
        field: 'import.handleFieldAria',
        errMissing: 'import.errHandleMissing',
        errShape: 'import.errHandleShape'
      }
    }
  },
  moobot: {
    id: 'moobot',
    label: 'Moobot',
    initials: 'M',
    chip: 'file',
    available: true,
    i18n: { desc: 'import.moobotDesc', instr: 'import.instrMoobot' },
    input: {
      kind: 'file',
      accept: '.json,application/json',
      maxBytes: MAX_MOOBOT_BYTES,
      parseInBrowser: parseMoobot
    }
  },
  nightbot: {
    id: 'nightbot',
    label: 'Nightbot',
    initials: 'NB',
    chip: 'connect',
    available: true,
    i18n: { desc: 'import.nightbotDesc', instr: 'import.instrNightbot' },
    input: {
      kind: 'oauth',
      connectPath: '/settings/import/nightbot/connect',
      i18n: {
        cta: 'import.nbConnectCta',
        connected: 'import.nbConnected',
        scopeHint: 'import.nbScopeHint',
        errNotConnected: 'import.errNbNotConnected'
      },
      errorParams: { nb_oauth: 'import.errNbOauth', nb_config: 'import.errNbConfig' }
    }
  },
  streamlabs_desktop: {
    id: 'streamlabs_desktop',
    label: 'StreamLabs Chatbot',
    initials: 'SL',
    chip: 'file',
    available: true,
    i18n: { desc: 'import.slDesc', instr: 'import.instrSl' },
    input: {
      kind: 'file',
      accept: '.db,application/octet-stream',
      maxBytes: MAX_STREAMLABS_BYTES
    }
  },
  wizebot: {
    id: 'wizebot',
    label: 'Wizebot',
    initials: 'WZ',
    chip: 'handle',
    available: true,
    i18n: { desc: 'import.wizebotDesc', instr: 'import.instrWizebot' },
    input: {
      kind: 'text',
      secret: false,
      shape: WIZEBOT_HANDLE_SHAPE,
      maxLen: MAX_WIZEBOT_HANDLE_LEN,
      placeholder: 'yourchannel',
      i18n: {
        field: 'import.handleFieldAria',
        errMissing: 'import.errHandleMissing',
        errShape: 'import.errHandleShape'
      }
    }
  }
};

export function strategyOf(id: ImportSource): ImportSourceStrategy {
  return IMPORT_STRATEGIES[id];
}

export function isImportSource(v: string): v is ImportSource {
  return Object.prototype.hasOwnProperty.call(IMPORT_STRATEGIES, v);
}
