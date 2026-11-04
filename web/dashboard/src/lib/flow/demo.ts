// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { dev } from '$app/env';
import type { Flow } from './model';

if (!dev) throw new Error('DASHBOARD_DEV_FIXTURE_INCLUDED_IN_PRODUCTION');

export type DemoSystem = {
  id: string;
  name: string;
  section: string;
  description: string;
  flow: Flow;
};

const flowOf = (id: string, name: string, nodes: Flow['nodes'], edges: Flow['edges']): Flow => ({
  id,
  name,
  enabled: true,
  nodes,
  edges
});

const node = (id: string, type: string, config: Flow['nodes'][number]['config'] = {}) => ({
  id,
  type,
  x: 0,
  y: 0,
  config
});

const edge = (id: string, from: string, port: string, to: string) => ({ id, from, port, to });

export const DEMO_SYSTEMS: DemoSystem[] = [
  {
    id: 'hug',
    name: '!hug command',
    section: 'Commands',
    description: 'Hugs someone, most of the time',
    flow: flowOf(
      'hug',
      '!hug command',
      [
        node('a1', 'twitch.chat.command', { trigger: '!hug' }),
        node('a2', 'cond.chance', { percent: 85 }),
        node('a3', 'reply.chat', { message: '{user} gives {touser} a big warm hug.' }),
        node('a4', 'reply.chat', { message: 'The hug slipped away, {user}. Try again!' })
      ],
      [edge('ae1', 'a1', 'next', 'a2'), edge('ae2', 'a2', 'yes', 'a3'), edge('ae3', 'a2', 'no', 'a4')]
    )
  },
  {
    id: 'follow',
    name: 'Follow alert',
    section: 'Alerts',
    description: 'Welcomes every new follower',
    flow: flowOf(
      'follow',
      'Follow alert',
      [
        node('f1', 'twitch.follow'),
        node('f2', 'reply.chat', { message: 'Welcome to the stream, {user}!' }),
        node('f3', 'reply.points', { amount: 100 })
      ],
      [edge('fe1', 'f1', 'next', 'f2'), edge('fe2', 'f1', 'next', 'f3')]
    )
  },
  {
    id: 'sub',
    name: 'Sub alert',
    section: 'Alerts',
    description: 'Thanks subs, with extra love for long ones',
    flow: flowOf(
      'sub',
      'Sub alert',
      [
        node('s1', 'twitch.sub', { variant: 'any' }),
        node('s2', 'cond.compare', { left: '{months}', op: 'gte', right: '12' }),
        node('s3', 'reply.chat', { message: '{user} has been here {months} months. Legend!' }),
        node('s4', 'reply.chat', { message: 'Thanks for the sub, {user}!' })
      ],
      [edge('se1', 's1', 'next', 's2'), edge('se2', 's2', 'yes', 's3'), edge('se3', 's2', 'no', 's4')]
    )
  },
  {
    id: 'raid',
    name: 'Raid shoutout',
    section: 'Alerts',
    description: 'Shouts out raids of five viewers or more',
    flow: flowOf(
      'raid',
      'Raid shoutout',
      [
        node('r1', 'twitch.raid', { minViewers: 1 }),
        node('r2', 'cond.compare', { left: '{viewers}', op: 'gte', right: '5' }),
        node('r3', 'reply.chat', { message: 'Welcome raiders! {user} brought {viewers} friends.' }),
        node('r4', 'reply.discord', { channel: 'announcements', message: '{user} just raided with {viewers} viewers!' })
      ],
      [edge('re1', 'r1', 'next', 'r2'), edge('re2', 'r2', 'yes', 'r3'), edge('re3', 'r2', 'yes', 'r4')]
    )
  },
  {
    id: 'hydrate',
    name: 'Hydrate reward',
    section: 'Channel point rewards',
    description: 'Water break reminder with a follow-up',
    flow: flowOf(
      'hydrate',
      'Hydrate reward',
      [
        node('h1', 'twitch.reward', { reward: 'hydrate' }),
        node('h2', 'reply.chat', { message: 'Sip time, {user}! Water break for everyone.' }),
        node('h3', 'reply.wait', { seconds: 30 }),
        node('h4', 'reply.chat', { message: 'Did everyone drink? Thanks {user}!' })
      ],
      [edge('he1', 'h1', 'next', 'h2'), edge('he2', 'h2', 'next', 'h3'), edge('he3', 'h3', 'next', 'h4')]
    )
  },
  {
    id: 'cheer',
    name: 'Big cheer celebration',
    section: 'Automations',
    description: 'Lights and thanks for big bit cheers',
    flow: flowOf(
      'cheer',
      'Big cheer celebration',
      [
        node('n1', 'twitch.cheer', { minBits: 100 }),
        node('n2', 'cond.compare', { left: '{bits}', op: 'gte', right: '1000' }),
        node('n3', 'reply.chat', { message: 'HUGE thanks {user} for {bits} bits!' }),
        node('n4', 'reply.lights', { mode: 'color', color: '#f5a524' }),
        node('n5', 'reply.chat', { message: 'Thanks for the bits, {user}!' })
      ],
      [edge('e1', 'n1', 'next', 'n2'), edge('e2', 'n2', 'yes', 'n3'), edge('e3', 'n2', 'yes', 'n4'), edge('e4', 'n2', 'no', 'n5')]
    )
  }
];
