// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface EventField {
  name: string;
  path: string;
  numeric: boolean;
}

export interface EventKind {
  kind: string;
  fields: EventField[];
}

const text = (name: string, path: string): EventField => ({ name, path, numeric: false });
const num = (name: string, path: string): EventField => ({ name, path, numeric: true });

const viewer = (...extra: EventField[]): EventField[] => [
  text('user', 'user_name'),
  text('login', 'user_login'),
  text('user_id', 'user_id'),
  ...extra,
];

const kind = (name: string, fields: EventField[] = []): EventKind => ({ kind: name, fields });

const KINDS: EventKind[] = [
  kind('channel.follow', viewer()),
  kind('channel.subscribe', viewer(text('tier', 'tier'), text('is_gift', 'is_gift'))),
  kind(
    'channel.subscription.message',
    viewer(text('tier', 'tier'), num('months', 'cumulative_months'), num('streak', 'streak_months'), text('message', 'message.text')),
  ),
  kind(
    'channel.subscription.gift',
    viewer(text('tier', 'tier'), num('amount', 'total'), num('total', 'cumulative_total'), text('anonymous', 'is_anonymous')),
  ),
  kind('channel.cheer', viewer(num('amount', 'bits'), text('message', 'message'), text('anonymous', 'is_anonymous'))),
  kind('channel.raid', [
    text('user', 'from_broadcaster_user_name'),
    text('login', 'from_broadcaster_user_login'),
    text('user_id', 'from_broadcaster_user_id'),
    num('viewers', 'viewers'),
  ]),
  kind('channel.ad_break.begin', [num('seconds', 'duration_seconds'), text('automatic', 'is_automatic')]),
  kind(
    'channel.channel_points_custom_reward_redemption.add',
    viewer(text('reward', 'reward.title'), text('reward_id', 'reward.id'), num('cost', 'reward.cost'), text('input', 'user_input')),
  ),
  kind('stream.online', [text('type', 'type')]),
  kind('stream.offline'),
];

const BY_KIND = new Map(KINDS.map((k) => [k.kind, k]));

export function eventKinds(): EventKind[] {
  return KINDS.map((k) => ({ kind: k.kind, fields: k.fields.map((f) => ({ ...f })) })).sort((a, b) =>
    a.kind < b.kind ? -1 : a.kind > b.kind ? 1 : 0,
  );
}

export function lookupEventKind(name: string): EventKind | undefined {
  return BY_KIND.get(name);
}
