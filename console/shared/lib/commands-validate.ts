// Command validation shared by the dashboard server action and the client
// editor, so the instant client-side feedback and the authoritative server
// check can never disagree.
//
// Normalization mirrors the commands service: the stored key never carries the
// leading "!" and is lower-case; chat keeps the "!" to invoke.

export const COMMAND_NAME_MAX = 64;
/** Per line — each line is sent as its own chat message (Twitch limit). */
export const RESPONSE_MAX = 500;
/** A response is newline-delimited: the bot sends one message per line. */
export const RESPONSE_MAX_LINES = 5;
export const COOLDOWN_MAX = 86400;

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
    .map((l) => l.replace(/[ \t]+$/, ''))
    .filter((l) => l !== '');
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

function nameProblem(name: string, what: string): string | undefined {
  if (!name) return `${what} is required.`;
  if (name.length > COMMAND_NAME_MAX) return `${what} must be at most ${COMMAND_NAME_MAX} characters.`;
  if (/\s/.test(name)) return `${what} cannot contain spaces.`;
  if (name.includes('!')) return `${what} only carries the "!" in chat — leave it out here.`;
  return undefined;
}

export function validateCommand(f: CommandFields): CommandErrors {
  const errors: CommandErrors = {};

  const nameErr = nameProblem(f.name, 'Command name');
  if (nameErr) errors.name = nameErr;

  const seen = new Set<string>([f.name]);
  for (const a of f.aliases) {
    const aliasErr = nameProblem(a, `Alternate name "${a}"`);
    if (aliasErr) {
      errors.aliases = aliasErr;
      break;
    }
    if (a === f.name) {
      errors.aliases = `"${a}" is already the command's own name.`;
      break;
    }
    if (seen.has(a)) {
      errors.aliases = `"${a}" is listed twice.`;
      break;
    }
    seen.add(a);
  }

  const lines = responseLines(f.response);
  if (lines.length === 0) errors.response = 'Response is required.';
  else if (lines.length > RESPONSE_MAX_LINES) {
    errors.response = `Response can be at most ${RESPONSE_MAX_LINES} lines — each line is sent as its own chat message.`;
  } else if (lines.some((l) => l.length > RESPONSE_MAX)) {
    errors.response = `Each line must be at most ${RESPONSE_MAX} characters.`;
  }

  if (!Number.isFinite(f.cooldown) || f.cooldown < 0 || f.cooldown > COOLDOWN_MAX) {
    errors.cooldown = `Cooldown must be between 0 and ${COOLDOWN_MAX} seconds.`;
  } else if (!Number.isInteger(f.cooldown)) {
    errors.cooldown = 'Cooldown must be a whole number of seconds.';
  }

  if (f.allowedUserId && !/^[0-9]+$/.test(f.allowedUserId)) {
    errors.allowed_user_id = 'User restriction must be a numeric Twitch user id.';
  }

  return errors;
}

/** Convenience: the first message of an error map, for single-line surfaces. */
export function firstError(errors: CommandErrors): string | undefined {
  return Object.values(errors)[0];
}
