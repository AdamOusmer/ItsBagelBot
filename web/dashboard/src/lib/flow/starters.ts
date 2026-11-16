// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { newFlow, type Flow } from './model';
import type { Translate } from './messages';

export const STARTERS = ['hug', 'follow', 'sub'] as const;
export type StarterId = typeof STARTERS[number];

export function starterFlow(id: string | null, t: Translate): Flow | null {
  const starter = STARTERS.find((starter) => starter === id);
  if (!starter) return null;
  const flow = newFlow(t(`flows.starters.${starter}.title`));
  const triggers = {
    hug: { type: 'twitch.chat.command', config: { trigger: '!hug' } },
    follow: { type: 'twitch.follow', config: {} },
    sub: { type: 'twitch.sub', config: { variant: 'any' } }
  } satisfies Record<StarterId, Pick<Flow['nodes'][number], 'type' | 'config'>>;
  flow.nodes = [
    { id: 'start', ...triggers[starter], x: 0, y: 0 },
    { id: 'reply', type: 'reply.chat', config: { message: t(`flows.starters.${starter}.message`, { user: '{user}', touser: '{touser}' }) }, x: 0, y: 0 }
  ];
  flow.edges = [{ id: 'start-reply', from: 'start', port: 'next', to: 'reply' }];
  return flow;
}
