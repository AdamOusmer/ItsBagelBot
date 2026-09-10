// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Client-safe registry of import sources: one strategy object per source,
// carrying everything the picker and the instructions step need in order to
// render and validate that source without naming it.
//
// Before this (2026-09-07) the import page carried five parallel lookup tables
// (PICKABLE, CLIENT_PARSED, SOURCE_LABEL, SOURCE_INITIALS, INSTR_KEY) plus one
// `source === '…'` branch per input kind, and the form action carried two more
// (SOURCE_INPUT_RULES, resolveCredential) with an
// `Exclude<ImportSource, 'fossabot'>` carve-out threaded through both. Adding a
// source meant editing every one of them and remembering the carve-out. A
// source is now one object here plus one object in the server registry
// (dashboard/src/lib/server/importer/strategy.ts).
//
// Client-safe is load-bearing: +page.svelte imports this module, so it may only
// pull in what that route's browser bundle already pays for. parseMoobot is a
// static import because the page already bundles it (the "import page (moobot +
// caps)" entry of shared/scripts/size.ts measures exactly that). The
// StreamElements, Nightbot and StreamLabs parsers stay server-side and are
// reached through the server registry, never from here.
import type { MessageKey } from '../i18n/keys';
import { parseMoobot } from './moobot';
import type { ImportDiagnostic, ImportManifest, ImportSource } from './types';

// ParseResult is what every source parser returns: the translated manifest
// plus the per-item findings collected while translating it.
export interface ParseResult {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
}

// TextInputSpec describes a source whose input is pasted into the page: a
// secret (a StreamElements JWT) or a public channel handle. `shape` and
// `maxLen` are the same gate the server applies, run client-side so an obvious
// typo is answered without a round trip.
export interface TextInputSpec {
  kind: 'text';
  secret: boolean;
  shape: RegExp;
  maxLen: number;
  placeholder: string;
  // linkHref/linkLabel render the "where do I find this" deep link above the
  // field; both or neither.
  linkHref?: string;
  linkLabel?: MessageKey;
  i18n: { field: MessageKey; hint?: MessageKey; errMissing: MessageKey; errShape: MessageKey };
}

// FileInputSpec describes a source whose export is a file. parseInBrowser is
// set only when the export can be decoded in the page (Moobot JSON), which is
// what keeps the raw file off the wire; without it the file uploads and the
// server's leg parses it.
export interface FileInputSpec {
  kind: 'file';
  accept: string;
  maxBytes: number;
  parseInBrowser?: (bytes: Uint8Array) => ParseResult;
}

// OAuthInputSpec describes a source the user connects to instead of pasting
// anything. errorParams maps the `?e=` values the OAuth routes bounce back
// with onto the prose the instructions step shows inline.
export interface OAuthInputSpec {
  kind: 'oauth';
  connectPath: string;
  i18n: { cta: MessageKey; connected: MessageKey; scopeHint: MessageKey; errNotConnected: MessageKey };
  errorParams: Record<string, MessageKey>;
}

export type InputSpec = TextInputSpec | FileInputSpec | OAuthInputSpec;

// ChipKind names the one-word badge on a source tile: what the user has to
// bring. 'handle' is a public channel name (no secret, no file, no connect).
export type ChipKind = 'token' | 'file' | 'connect' | 'handle';

// CHIP_LABEL_KEYS translates a chip kind for an AVAILABLE source; an
// unavailable one renders import.chipSoon instead and never reaches this map.
export const CHIP_LABEL_KEYS: Record<ChipKind, MessageKey> = {
  token: 'import.chipToken',
  file: 'import.chipFile',
  connect: 'import.chipConnect',
  handle: 'import.chipHandle'
};

export interface ImportSourceStrategy {
  id: ImportSource;
  // label is the bot's own spelling ('StreamElements'), used verbatim in tile
  // names, headings and refusal prose, so nothing hard-codes a source name.
  label: string;
  // initials fill the tile glyph.
  initials: string;
  chip: ChipKind;
  // available: false ships the tile visibly disabled and refuses direct posts
  // server-side. It is not a feature flag: it says the source has no working
  // input yet.
  available: boolean;
  // instr is absent for a source with no instructions step to reach (an
  // unavailable one), never empty-keyed.
  i18n: { desc: MessageKey; instr?: MessageKey };
  input: InputSpec;
}

// JWT_SHAPE mirrors the gate in ./streamelements (three dot-separated base64url
// segments, MAX_CREDENTIAL_LEN 4096): checking it here means a mistyped paste
// is answered before a fetch is attempted, and guarantees no credential with
// interior whitespace or control characters reaches the transport.
const JWT_SHAPE = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;
const MAX_JWT_LEN = 4096;

// FOSSABOT_HANDLE_SHAPE mirrors the slug gate in importer/fossabot/fetch.ts
// (applied there after trim().toLowerCase(), hence the case-insensitive
// spelling here): a Twitch login plus the dotted/hyphenated aliases Fossabot
// accepts. Exported because the server's acceptInput checks the same shape,
// and one regex in two files is one regex that drifts.
export const FOSSABOT_HANDLE_SHAPE = /^[A-Za-z0-9_.-]{1,64}$/;
export const MAX_HANDLE_LEN = 64;

// WIZEBOT_HANDLE_SHAPE mirrors HANDLE_SHAPE in ./wizebot/fetch, restated here
// rather than imported so the picker's client bundle never pulls in a
// server-only fetch layer. It is the subdomain of the channel's streaming
// website (<login>.streaming.lv), which is a Twitch login with "-" where the
// login has "_": 25 characters at most, no leading dash.
const WIZEBOT_HANDLE_SHAPE = /^[A-Za-z0-9][A-Za-z0-9-]{0,24}$/;
const MAX_WIZEBOT_HANDLE_LEN = 25;

// Browser-side ceiling on a Moobot export, mirrored by the server's own
// MAX_UPLOAD_BYTES: 10 MiB of JSON is already an order of magnitude past the
// largest real export, and refusing here means a hostile file is never read.
const MAX_MOOBOT_BYTES = 10 * 1024 * 1024;

// StreamLabs' Chatbot.db still uploads whole because console CSP forbids WASM
// (no 'wasm-unsafe-eval' in script-src, see shared/svelte-config.js), which
// rules out an in-browser SQLite reader. 20 MiB matches the action's ceiling.
const MAX_STREAMLABS_BYTES = 20 * 1024 * 1024;

// IMPORT_STRATEGIES is the client-side source of truth. Iterate it through
// IMPORT_SOURCES (./types), which fixes the tile order.
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
        // No shape-specific string exists yet, and the layer that adds handle
        // sources adds one: until then a malformed paste reads as a missing
        // one rather than inventing copy that the locales cannot translate.
        errShape: 'import.errJwtMissing'
      }
    }
  },
  fossabot: {
    id: 'fossabot',
    label: 'Fossabot',
    initials: 'F',
    chip: 'handle',
    // The input is a public channel name, not a secret: the parser reads the
    // same commands directory any viewer can open at fossabot.com/<login>.
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

// isImportSource narrows a form field or query parameter onto the registry.
// Both sides of the wire use it, so an unknown string is rejected in exactly
// one shape.
export function isImportSource(v: string): v is ImportSource {
  return Object.prototype.hasOwnProperty.call(IMPORT_STRATEGIES, v);
}
