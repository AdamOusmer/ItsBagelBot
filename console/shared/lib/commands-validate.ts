// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Command validation shared by the dashboard server action and the client
// editor, so the instant client-side feedback and the authoritative server
// check can never disagree.
//
// Normalization mirrors the commands service: the stored key never carries the
// leading "!" and is lower-case; chat keeps the "!" to invoke.

// The $(urlfetch) definition validators moved to fetch-validate.ts when they
// outgrew this file; the re-export keeps every existing import path working.
export * from './fetch-validate';
import { urlFetchNames, URLFETCH_TOKEN_CAP, type FetchDefErrors } from './fetch-validate';

export const COMMAND_NAME_MAX = 64;
/** Per line — each line is sent as its own chat message (Twitch limit). */
export const RESPONSE_MAX = 500;
/** A response is newline-delimited: the bot sends one message per line. */
export const RESPONSE_MAX_LINES = 5;
export const COOLDOWN_MAX = 86400;

// --- urlfetch definition rules ---------------------------------------------
//
// The numbers below are the shared contract between this console (instant
// client feedback AND the authoritative server re-check in the fetches page
// actions) and the commands/gossip services' Go validators. They live here —
// not inline in the UI — so client and server literally cannot drift.

/** The bare command trigger: drop a leading "!" and lower-case. */
export function normName(s: string): string {
  return s.trim().replace(/^!+/, '').trim().toLowerCase();
}

/**
 * The response's meaningful lines, mirroring the commands service's
 * normalization: CRLF folds to LF, trailing whitespace per line and blank
 * lines are dropped. Shared by the validator, the editor's counters and the
 * chat rehearsal so all three agree on what actually gets sent.
 */
export function responseLines(response: string): string[] {
  return response
    .split(/\r\n|\r|\n/)
    .map(trimLineEnd)
    .filter((l) => l !== '');
}

/** Canonical wire/storage form: one non-empty chat message per LF-delimited line. */
export function normalizeCommandResponse(response: string): string {
  return responseLines(response).join('\n');
}

// Linear-time right-trim of spaces/tabs (mirrors Go's TrimRight(" \t")); a
// trailing-whitespace regex backtracks polynomially on adversarial input.
const LINE_TRAILERS = new Set([' ', '\t']);

function trimLineEnd(line: string): string {
  let end = line.length;
  while (end > 0 && LINE_TRAILERS.has(line[end - 1])) end--;
  return line.slice(0, end);
}

export interface CommandFields {
  /** Normalized (normName) trigger. */
  name: string;
  /** Normalized, de-duplicated alternate names. */
  aliases: string[];
  response: string;
  cooldown: number;
  /** Digits-only Twitch user id, or '' for unrestricted. */
  allowedUserId: string;
}

/** field -> human message; empty object = valid. Keys match form field names. */
export type CommandErrors = Partial<
  Record<'name' | 'aliases' | 'response' | 'cooldown' | 'allowed_user_id', string>
>;

// NameCheck is one trigger-shaped value under validation with the label its
// error prose should carry ("Command name", "Alternate name …").
interface NameCheck {
  value: string;
  what: string;
}

function nameProblem({ value, what }: NameCheck): string | undefined {
  if (!value) return `${what} is required.`;
  if (value.length > COMMAND_NAME_MAX) return `${what} must be at most ${COMMAND_NAME_MAX} characters.`;
  if (/\s/.test(value)) return `${what} cannot contain spaces.`;
  if (value.includes('!')) return `${what} only carries the "!" in chat — leave it out here.`;
  return undefined;
}

// Per-field problem functions, the validateFetchDef shape: each owns one
// field's rules and returns the first violation, so the assembler spends one
// branch per field.
function aliasesProblem(name: string, aliases: string[]): string | undefined {
  const seen = new Set<string>([name]);
  for (const a of aliases) {
    const aliasErr = nameProblem({ value: a, what: `Alternate name "${a}"` });
    if (aliasErr) return aliasErr;
    if (a === name) return `"${a}" is already the command's own name.`;
    if (seen.has(a)) return `"${a}" is listed twice.`;
    seen.add(a);
  }
  return undefined;
}

function responseProblem(response: string): string | undefined {
  const lines = responseLines(response);
  if (lines.length === 0) return 'Response is required.';
  if (lines.length > RESPONSE_MAX_LINES) {
    return `Response can be at most ${RESPONSE_MAX_LINES} lines — each line is sent as its own chat message.`;
  }
  if (lines.some((l) => l.length > RESPONSE_MAX)) return `Each line must be at most ${RESPONSE_MAX} characters.`;
  if (lines.some((l) => CONTROL_CHAR_RE.test(l))) return 'Response cannot contain control characters.';
  if (urlFetchNames(response).length > URLFETCH_TOKEN_CAP) {
    // Distinct names, not occurrences: the engine dedupes repeats before the
    // fan-out, so the latency budget it must absorb scales with distinct defs.
    return `A response can reference at most ${URLFETCH_TOKEN_CAP} different fetched values ({urlfetch:…}).`;
  }
  return undefined;
}

function cooldownProblem(cooldown: number): string | undefined {
  // The negated range check refuses NaN and both infinities in one shape:
  // NaN fails every comparison, ±Infinity falls outside the bounds.
  if (!(cooldown >= 0 && cooldown <= COOLDOWN_MAX)) return `Cooldown must be between 0 and ${COOLDOWN_MAX} seconds.`;
  if (!Number.isInteger(cooldown)) return 'Cooldown must be a whole number of seconds.';
  return undefined;
}

export function validateCommand(f: CommandFields): CommandErrors {
  const errors: CommandErrors = {};
  const name = nameProblem({ value: f.name, what: 'Command name' });
  if (name) errors.name = name;
  const aliases = aliasesProblem(f.name, f.aliases);
  if (aliases) errors.aliases = aliases;
  const response = responseProblem(f.response);
  if (response) errors.response = response;
  const cooldown = cooldownProblem(f.cooldown);
  if (cooldown) errors.cooldown = cooldown;
  if (f.allowedUserId && !/^[0-9]+$/.test(f.allowedUserId)) {
    errors.allowed_user_id = 'User restriction must be a numeric Twitch user id.';
  }
  return errors;
}

// Plain character class: linear, no backtracking (the same rune set the
// byte loop it replaced checked — C0 controls).
const CONTROL_CHAR_RE = /[\u0000-\u001f]/;

/** Convenience: the first message of an error map, for single-line surfaces. */
export function firstError(errors: CommandErrors | FetchDefErrors): string | undefined {
  return Object.values(errors)[0];
}
