// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export * from './fetch-validate';
import { urlFetchNames, URLFETCH_TOKEN_CAP, type FetchDefErrors } from './fetch-validate';
import { bumpCounterProblem } from './counter-validate';

export const COMMAND_NAME_MAX = 64;
export const RESPONSE_MAX = 500;
export const RESPONSE_MAX_LINES = 5;
export const COOLDOWN_MAX = 86400;

export function normName(s: string): string {
  return s.trim().replace(/^!+/, '').trim().toLowerCase();
}

export function responseLines(response: string): string[] {
  return response
    .split(/\r\n|\r|\n/)
    .map(trimLineEnd)
    .filter((l) => l !== '');
}

export function normalizeCommandResponse(response: string): string {
  return responseLines(response).join('\n');
}

const LINE_TRAILERS = new Set([' ', '\t']);

function trimLineEnd(line: string): string {
  let end = line.length;
  while (end > 0 && LINE_TRAILERS.has(line[end - 1])) end--;
  return line.slice(0, end);
}

export interface CommandFields {
  name: string;
  aliases: string[];
  response: string;
  cooldown: number;
  allowedUserId: string;
  bumpCounter: string;
}

export type CommandErrors = Partial<
  Record<'name' | 'aliases' | 'response' | 'cooldown' | 'allowed_user_id' | 'bump_counter', string>
>;

interface NameCheck {
  value: string;
  what: string;
}

function nameProblem({ value, what }: NameCheck): string | undefined {
  if (!value) return `${what} is required.`;
  if (value.length > COMMAND_NAME_MAX) return `${what} must be at most ${COMMAND_NAME_MAX} characters.`;
  if (/\s/.test(value)) return `${what} cannot contain spaces.`;
  if (value.includes('!')) return `${what} only carries the "!" in chat. Leave it out here.`;
  return undefined;
}

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
    return `Response can be at most ${RESPONSE_MAX_LINES} lines. Each line is sent as its own chat message.`;
  }
  if (lines.some((l) => l.length > RESPONSE_MAX)) return `Each line must be at most ${RESPONSE_MAX} characters.`;
  if (lines.some((l) => CONTROL_CHAR_RE.test(l))) return 'Response cannot contain control characters.';
  if (urlFetchNames(response).length > URLFETCH_TOKEN_CAP) {
    return `A response can reference at most ${URLFETCH_TOKEN_CAP} different fetched values ({urlfetch:…}).`;
  }
  return undefined;
}

function cooldownProblem(cooldown: number): string | undefined {
  const inRangeAndNotNaN = cooldown >= 0 && cooldown <= COOLDOWN_MAX;
  if (!inRangeAndNotNaN) return `Cooldown must be between 0 and ${COOLDOWN_MAX} seconds.`;
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
  const bumpCounter = bumpCounterProblem(f.bumpCounter);
  if (bumpCounter) errors.bump_counter = bumpCounter;
  return errors;
}

const CONTROL_CHAR_RE = /[\u0000-\u001f]/;

export function firstError(errors: CommandErrors | FetchDefErrors): string | undefined {
  return Object.values(errors)[0];
}
