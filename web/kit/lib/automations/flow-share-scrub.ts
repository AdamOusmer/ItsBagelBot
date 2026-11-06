// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type ScrubKind = 'url_credentials' | 'known_token' | 'jwt' | 'high_entropy';

export interface ScrubbedText {
  text: string;
  kinds: ScrubKind[];
}

const URL_PATTERN = /\b[a-z][a-z0-9+.-]*:\/\/[^\s"'<>]+/gi;
const USERINFO = /^([a-z][a-z0-9+.-]*:\/\/)([^/?#]*)@/i;
const URL_PARAM = /([?&#])([^=&#]+)=([^&#]*)/g;
const SECRET_PARAM = /(^|[_-])(token|secret|password|passwd|pwd|auth|authorization|apikey|key|signature|sig|credentials?|session)$/i;

const KNOWN_TOKENS: readonly RegExp[] = [
  /\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}/g,
  /\bgithub_pat_[A-Za-z0-9_]{20,}/g,
  /\bsk-[A-Za-z0-9_-]{20,}/g,
  /\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}/g,
  /\bxox[abeoprs]-[A-Za-z0-9-]{10,}/g,
  /\bxapp-[A-Za-z0-9-]{10,}/g,
  /\b(?:AKIA|ASIA)[0-9A-Z]{16}\b/g,
  /\bAIza[A-Za-z0-9_-]{35}/g,
  /\bglpat-[A-Za-z0-9_-]{20,}/g,
  /\bnpm_[A-Za-z0-9]{30,}/g,
  /\boauth:[A-Za-z0-9]{20,}/g,
  /\bBearer +(?=[A-Za-z._~+/=-]*[0-9])[A-Za-z0-9._~+/=-]{16,}/gi
];
const JWT = /\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*/g;

const LONG_RUN = /[A-Za-z0-9+=_-]{24,}/g;
const RUN_SEPARATORS = /[_-]/;
const MIN_SEGMENT = 24;
const MIN_DIGIT_SWITCHES = 4;
const MIN_BITS_PER_CHAR = 3;

interface UrlParam {
  name: string;
  value: string;
}

interface RemovalRule {
  pattern: RegExp;
  kind: ScrubKind;
}

const isSecretParam = ({ name, value }: UrlParam) => value !== '' && !value.includes('{') && SECRET_PARAM.test(name);

function bitsPerChar(chars: readonly string[]): number {
  const counts = new Map<string, number>();
  for (const char of chars) counts.set(char, (counts.get(char) ?? 0) + 1);
  let bits = 0;
  for (const count of counts.values()) bits -= (count / chars.length) * Math.log2(count / chars.length);
  return bits;
}

function digitSwitches(chars: readonly string[]): number {
  let switches = 0;
  for (let i = 1; i < chars.length; i++) {
    if (/[0-9]/.test(chars[i]!) !== /[0-9]/.test(chars[i - 1]!)) switches++;
  }
  return switches;
}

// Slugs and uuids split into short segments, and a title ending in a year switches between letters and digits once.
function looksRandom(segments: readonly string[]): boolean {
  return segments.some((segment) => {
    if (segment.length < MIN_SEGMENT) return false;
    const chars = segment.split('');
    return digitSwitches(chars) >= MIN_DIGIT_SWITCHES && bitsPerChar(chars) >= MIN_BITS_PER_CHAR;
  });
}

class SecretScrub {
  readonly kinds = new Set<ScrubKind>();
  text = '';

  urlsOf(input: string): void {
    this.text = input.replace(URL_PATTERN, (url) => {
      const clean = url.replace(USERINFO, (whole, scheme: string, info: string) => {
        if (info.includes('{')) return whole;
        this.kinds.add('url_credentials');
        return scheme;
      });
      return clean.replace(URL_PARAM, (whole, mark: string, name: string, value: string) => {
        if (!isSecretParam({ name, value })) return whole;
        this.kinds.add('url_credentials');
        return `${mark}${name}=`;
      });
    });
  }

  remove({ pattern, kind }: RemovalRule): void {
    this.text = this.text.replace(pattern, () => {
      this.kinds.add(kind);
      return '';
    });
  }

  removeRandomRuns(): void {
    this.text = this.text.replace(LONG_RUN, (run) => {
      if (!looksRandom(run.split(RUN_SEPARATORS))) return run;
      this.kinds.add('high_entropy');
      return '';
    });
  }
}

export function scrubSecrets(input: string): ScrubbedText {
  const scrub = new SecretScrub();
  scrub.urlsOf(input);
  for (const pattern of KNOWN_TOKENS) scrub.remove({ pattern, kind: 'known_token' });
  scrub.remove({ pattern: JWT, kind: 'jwt' });
  scrub.removeRandomRuns();
  return { text: scrub.text, kinds: [...scrub.kinds] };
}
