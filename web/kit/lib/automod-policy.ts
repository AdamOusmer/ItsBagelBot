// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Mirrors the Rust AutoMod's policy decoding (bagel-automod policy_snapshot.rs and
// static_gates.rs). Rust drops a channel's whole policy when any entry fails to
// compile, so one bad domain would silently switch off every rule; reject it at save.

export const AUTOMOD_TERM_KEYS = ['block_terms', 'nsfw_terms', 'slur_terms', 'racism_terms', 'spam_phrases'] as const;
export const AUTOMOD_DOMAIN_KEYS = ['blocked_domains', 'ip_logger_domains'] as const;
export const AUTOMOD_ACCOUNT_KEY = 'blocked_accounts';

export const AUTOMOD_MAX_RULES = 256;
export const AUTOMOD_MAX_TERM_CHARS = 128;
export const AUTOMOD_MAX_TERM_BYTES = 2048;
export const AUTOMOD_MAX_HOST_BYTES = 256;
export const AUTOMOD_MAX_CONFIG_BYTES = 16 * 1024;

export type AutomodIssueCode = 'term' | 'domain' | 'account' | 'tooMany' | 'tooLarge';
export interface AutomodIssue {
  code: AutomodIssueCode;
  field?: string;
  entry?: string;
}

/** Same split as Rust's split_list: commas or new lines, trimmed, empties dropped. */
export function splitAutomodList(value: string | undefined): string[] {
  return (value ?? '')
    .split(/[,\n]/)
    .map((v) => v.trim())
    .filter((v) => v !== '');
}

// Rust's search view drops invisible format characters before measuring a term.
const IGNORED = /[\p{Cf}\p{Cc}]/gu;

function validTerm(term: string): boolean {
  if (byteLength(term) > AUTOMOD_MAX_TERM_BYTES) return false;
  const visible = term.replace(IGNORED, '');
  const chars = [...visible.toLowerCase()].length;
  return chars > 0 && chars <= AUTOMOD_MAX_TERM_CHARS;
}

const DNS_LABEL = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i;
const IPV4 = /^(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/;

/** A bare host such as grabify.link, 1.2.3.4 or [::1]; no scheme, path, port or trailing dot. */
export function validAutomodDomain(value: string): boolean {
  if (!value || byteLength(value) > AUTOMOD_MAX_HOST_BYTES) return false;
  if (/[\s/@\\?#]/.test(value) || value.endsWith('.')) return false;
  if (value.startsWith('[')) {
    if (!value.endsWith(']')) return false;
    try {
      return new URL(`https://${value}`).hostname === value.toLowerCase();
    } catch {
      return false;
    }
  }
  if (value.includes(':')) return false;
  if (IPV4.test(value)) return true;
  let host: string;
  try {
    // Internationalized names are checked in their punycode form, as Rust does.
    host = new URL(`https://${value}`).hostname;
  } catch {
    return false;
  }
  return host.length <= 253 && host.split('.').every((label) => DNS_LABEL.test(label));
}

/** Twitch numeric user id: Rust matches the sender id, never a login. */
export function validAutomodAccount(value: string): boolean {
  return /^[1-9]\d{0,19}$/.test(value);
}

function byteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

/** First entry across `fields` that `valid` rejects, plus how many entries were seen. */
function scanLists(
  config: Record<string, string>,
  fields: readonly string[],
  code: AutomodIssueCode,
  valid: (entry: string) => boolean
): { issue: AutomodIssue | null; count: number } {
  let count = 0;
  for (const field of fields) {
    const list = splitAutomodList(config[field]);
    const entry = list.find((value) => !valid(value));
    if (entry !== undefined) return { issue: { code, field, entry }, count };
    count += list.length;
  }
  return { issue: null, count };
}

/** First reason the Rust AutoMod would refuse this config, or null when it compiles. */
export function automodConfigIssue(config: Record<string, string>): AutomodIssue | null {
  if (byteLength(JSON.stringify(config)) > AUTOMOD_MAX_CONFIG_BYTES) return { code: 'tooLarge' };
  const terms = scanLists(config, AUTOMOD_TERM_KEYS, 'term', validTerm);
  if (terms.issue) return terms.issue;
  const staticTerms = terms.count - splitAutomodList(config.block_terms).length;
  if (terms.count > AUTOMOD_MAX_RULES || staticTerms > AUTOMOD_MAX_RULES) return { code: 'tooMany' };
  const domains = scanLists(config, AUTOMOD_DOMAIN_KEYS, 'domain', validAutomodDomain);
  if (domains.issue) return domains.issue;
  const accounts = scanLists(config, [AUTOMOD_ACCOUNT_KEY], 'account', validAutomodAccount);
  if (accounts.issue) return accounts.issue;
  return domains.count + accounts.count > AUTOMOD_MAX_RULES ? { code: 'tooMany' } : null;
}
