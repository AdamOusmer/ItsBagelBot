// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { COMMAND_NAME_MAX, COOLDOWN_MAX, PERMS, normName, type Perm } from '@bagel/kit';
import type { CommandDraft } from './drafts';

const COMMAND_ALIASES_MAX = 25;
const RESPONSE_COLUMN_MAX_CHARS = 2504;

export const COMPOSE_PARAMS = ['compose', 'name', 'response', 'perm', 'cooldown', 'aliases', 'lang'] as const;

export type ComposeLink = Pick<CommandDraft, 'name' | 'aliases' | 'response' | 'perm' | 'cooldown'>;

const isPerm = (value: string): value is Perm => (PERMS as readonly string[]).includes(value);

export function parseComposeLink(params: URLSearchParams): ComposeLink {
  const text = (key: string) => params.get(key) ?? '';
  const aliases = text('aliases').split(',').map(normName).filter(Boolean);
  const perm = text('perm');
  const cooldown = Math.floor(Number(params.get('cooldown')) || 0);
  return {
    name: normName(text('name')).slice(0, COMMAND_NAME_MAX),
    aliases: [...new Set(aliases)].slice(0, COMMAND_ALIASES_MAX),
    response: text('response').slice(0, RESPONSE_COLUMN_MAX_CHARS),
    perm: isPerm(perm) ? perm : 'everyone',
    cooldown: Math.min(Math.max(cooldown, 0), COOLDOWN_MAX)
  };
}
