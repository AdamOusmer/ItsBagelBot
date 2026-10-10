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

const ENCODER = new TextEncoder();

// Rust's search view drops invisible format characters before measuring a term.
const IGNORED = /[\p{Cf}\p{Cc}]/gu;

function validTerm(term: string): boolean {
  if (ENCODER.encode(term).length > AUTOMOD_MAX_TERM_BYTES) return false;
  const chars = [...term.replace(IGNORED, '').toLowerCase()].length;
  return chars > 0 && chars <= AUTOMOD_MAX_TERM_CHARS;
}

// Everything Rust's canonical_host refuses before it parses the value.
const FORBIDDEN_HOST_CHARS = /[\s/@\\?#]/;
// Up to 253 characters of dot-separated labels, each 1-63 letters, digits or inner hyphens.
const DNS_NAME = /^(?=.{1,253}$)[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$/;

// Internationalized names come back in punycode, as Rust checks them.
function hostOf(value: string): string | null {
  try {
    return new URL(`https://${value}`).hostname;
  } catch {
    return null;
  }
}

/** A bare host such as grabify.link, 1.2.3.4 or [::1]; no scheme, path, port or trailing dot. */
export function validAutomodDomain(value: string): boolean {
  if (ENCODER.encode(value).length > AUTOMOD_MAX_HOST_BYTES || FORBIDDEN_HOST_CHARS.test(value) || value.endsWith('.')) return false;
  const host = hostOf(value);
  if (value.startsWith('[')) return value.endsWith(']') && host === value.toLowerCase();
  return !value.includes(':') && host !== null && DNS_NAME.test(host);
}

/** Twitch numeric user id: Rust matches the sender id, never a login. */
export function validAutomodAccount(value: string): boolean {
  return /^[1-9]\d{0,19}$/.test(value);
}

interface ListScan {
  issue: AutomodIssue | null;
  count: number;
}

/** First entry across `fields` that `valid` rejects, plus how many entries were seen. */
function scanLists(
  config: Record<string, string>,
  fields: readonly string[],
  code: AutomodIssueCode,
  valid: (entry: string) => boolean
): ListScan {
  let count = 0;
  for (const field of fields) {
    const list = splitAutomodList(config[field]);
    const entry = list.find((value) => !valid(value));
    if (entry !== undefined) return { issue: { code, field, entry }, count };
    count += list.length;
  }
  return { issue: null, count };
}

// Rust caps all terms, the categorized static terms, and sites plus accounts at 256 each.
function budgetIssue(terms: ListScan, blockTerms: number, others: number): AutomodIssue | null {
  const over = terms.count > AUTOMOD_MAX_RULES || terms.count - blockTerms > AUTOMOD_MAX_RULES || others > AUTOMOD_MAX_RULES;
  return over ? { code: 'tooMany' } : null;
}

/** First reason the Rust AutoMod would refuse this config, or null when it compiles. */
export function automodConfigIssue(config: Record<string, string>): AutomodIssue | null {
  if (ENCODER.encode(JSON.stringify(config)).length > AUTOMOD_MAX_CONFIG_BYTES) return { code: 'tooLarge' };
  const terms = scanLists(config, AUTOMOD_TERM_KEYS, 'term', validTerm);
  const domains = scanLists(config, AUTOMOD_DOMAIN_KEYS, 'domain', validAutomodDomain);
  const accounts = scanLists(config, [AUTOMOD_ACCOUNT_KEY], 'account', validAutomodAccount);
  const blockTerms = splitAutomodList(config.block_terms).length;
  return terms.issue ?? domains.issue ?? accounts.issue ?? budgetIssue(terms, blockTerms, domains.count + accounts.count);
}
