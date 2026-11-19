// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Mirrored in internal/domain/analytics/catalog.go (KindVia); a value outside this list is dropped.
export const VIA = [
  'chatgpt', 'gemini', 'claude', 'perplexity', 'copilot', 'google', 'bing', 'duckduckgo', 'yahoo',
  'twitch', 'youtube', 'reddit', 'x', 'discord', 'github', 'docs', 'other', 'direct', 'none'
] as const;

export type Via = (typeof VIA)[number];

export const VIA_PARAM = 'via';

const HOSTS: readonly (readonly [Via, readonly string[]])[] = [
  ['chatgpt', ['chatgpt.com', 'chat.openai.com']],
  ['gemini', ['gemini.google.com', 'bard.google.com']],
  ['claude', ['claude.ai']],
  ['perplexity', ['perplexity.ai']],
  ['copilot', ['copilot.microsoft.com']],
  ['bing', ['bing.com']],
  ['duckduckgo', ['duckduckgo.com']],
  ['yahoo', ['yahoo.com']],
  ['twitch', ['twitch.tv']],
  ['youtube', ['youtube.com', 'youtu.be']],
  ['reddit', ['reddit.com']],
  ['x', ['x.com', 'twitter.com', 't.co']],
  ['discord', ['discord.com', 'discord.gg']],
  ['github', ['github.com']],
  ['docs', ['docs.itsbagelbot.com', 'ai.itsbagelbot.com']],
  ['none', ['itsbagelbot.com']]
];

const GOOGLE = /(^|\.)google\.[a-z.]+$/;

function hostOf(value: string): string | null {
  try {
    return new URL(value.includes('://') ? value : `https://${value}`).hostname.toLowerCase();
  } catch {
    return null;
  }
}

function matches(host: string, domain: string): boolean {
  return host === domain || host.endsWith(`.${domain}`);
}

function fromHost(host: string): Via {
  const hit = HOSTS.find(([, domains]) => domains.some((d) => matches(host, d)));
  if (hit) return hit[0];
  return GOOGLE.test(host) ? 'google' : 'other';
}

export function isVia(value: string | null | undefined): value is Via {
  return (VIA as readonly string[]).includes(value ?? '');
}

export function classifyVia(referrer: string, utmSource: string | null): Via {
  const host = hostOf(utmSource || referrer);
  return host ? fromHost(host) : 'direct';
}
