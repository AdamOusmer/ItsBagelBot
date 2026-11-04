// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { dev } from '$app/env';
import { flowToDefinition, type FlowExportRow } from '@bagel/kit/automations';
import type { EditorData, Lowered } from '#lib/server/flow-page';
import { cardFromSummary, type FlowCard, type FlowSection } from './cards';
import { unavailable, type ChoiceList, type ChoiceOption } from './choices';
import { MAX_WAIT_SECONDS, specFor, type Flow } from './model';
import { flowRouteId, parseFlowRouteId, type FlowRef } from './ref';
import type { DeleteReply, SaveReply, ToggleReply } from './replies';
import { flowPreview } from './thumb';

if (!dev) throw new Error('DASHBOARD_DEV_FIXTURE_INCLUDED_IN_PRODUCTION');

export type DemoSystem = {
  id: string;
  name: string;
  section: string;
  description: string;
  flow: Flow;
};

const DEMO_STRIP = { device: 'AB:CD:EF:12:34:56', sku: 'H6159', name: 'Desk strip' };

type FlowId = Flow['id'];

type NodeId = Flow['nodes'][number]['id'];

const flowOf = (id: FlowId, name: Flow['name'], nodes: Flow['nodes'], edges: Flow['edges']): Flow => ({
  id,
  name,
  enabled: true,
  nodes,
  edges
});

const node = (id: NodeId, type: Flow['nodes'][number]['type'], config: Flow['nodes'][number]['config'] = {}) => ({
  id,
  type,
  x: 0,
  y: 0,
  config
});

const edge = (id: Flow['edges'][number]['id'], from: NodeId, port: Flow['edges'][number]['port'], to: NodeId) => ({ id, from, port, to });

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
        node('s1', 'twitch.sub', { variant: 'resub' }),
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
        node('r3', 'reply.chat', { message: 'Welcome raiders! {user} brought {viewers} friends.' })
      ],
      [edge('re1', 'r1', 'next', 'r2'), edge('re2', 'r2', 'yes', 'r3')]
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
        node('h3', 'reply.wait', { seconds: MAX_WAIT_SECONDS - 2 }),
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
        node('n4', 'reply.lights', { device: DEMO_STRIP.device, sku: DEMO_STRIP.sku, deviceName: DEMO_STRIP.name, mode: 'color', color: '#f5a524' }),
        node('n5', 'reply.chat', { message: 'Thanks for the bits, {user}!' })
      ],
      [edge('e1', 'n1', 'next', 'n2'), edge('e2', 'n2', 'yes', 'n3'), edge('e3', 'n2', 'yes', 'n4'), edge('e4', 'n2', 'no', 'n5')]
    )
  }
];

type DemoRow = { name: string; section: FlowSection; description: string; flow: Flow; enabled: boolean; revision: number };

const SECTIONS: Record<string, FlowSection> = {
  Commands: 'commands',
  Alerts: 'alerts',
  'Channel point rewards': 'rewards',
  Automations: 'automations'
};

const rows = new Map<string, DemoRow>(
  DEMO_SYSTEMS.map((system) => [
    system.id,
    { name: system.name, section: SECTIONS[system.section] ?? 'automations', description: system.description, flow: system.flow, enabled: true, revision: 1 }
  ])
);

let created = 0;

const TRIGGER_LABELS: Record<string, string> = {
  chatCommand: 'Chat command',
  chatWord: 'Chat word',
  follow: 'New follower',
  sub: 'Subscription',
  cheer: 'Cheer',
  raid: 'Raid',
  reward: 'Channel point reward',
  streamOnline: 'Stream goes live'
};

const triggerLabel = (flow: Flow) => TRIGGER_LABELS[specFor(flow.nodes.find((n) => specFor(n.type)?.kind === 'trigger')?.type ?? '')?.slug ?? ''] ?? '';

export function demoCards(): FlowCard[] {
  return [...rows].map(([id, row]) => ({
    ...cardFromSummary({ ref: { layer: 'automation', id }, name: row.name, enabled: row.enabled, revision: String(row.revision), preview: JSON.stringify(flowPreview(row.flow)) }),
    section: row.section,
    description: row.description,
    when: triggerLabel(row.flow)
  }));
}

export function demoEditor(routeId: string): EditorData | null {
  const ref = parseFlowRouteId(routeId);
  const row = ref?.layer === 'automation' ? rows.get(ref.id) : undefined;
  if (!ref || !row) return null;
  return { routeId, ref, revision: String(row.revision), name: row.name, flow: { ...structuredClone(row.flow), id: routeId, name: row.name, enabled: row.enabled } };
}

function demoCreate(lowered: Lowered): SaveReply {
  const id = `demo-${++created}`;
  const name = lowered.flow.name || 'Untitled';
  rows.set(id, { name, section: 'automations', description: '', flow: structuredClone(lowered.flow), enabled: lowered.flow.enabled, revision: 1 });
  return { ok: true, id: flowRouteId({ layer: 'automation', id }), name, enabled: lowered.flow.enabled, revision: '1', warnings: [] };
}

export function demoSave(lowered: Lowered): SaveReply {
  const { ref, expectedRevision } = lowered.request;
  if (!ref) return demoCreate(lowered);
  const row = ref.layer === 'automation' ? rows.get(ref.id) : undefined;
  if (!row || ref.layer !== 'automation') return { ok: false, reason: 'not_found' };
  if (expectedRevision !== undefined && Number(expectedRevision) !== row.revision) {
    return { ok: false, reason: 'conflict', issues: [], latestRevision: String(row.revision) };
  }
  row.name = lowered.flow.name || row.name;
  row.flow = structuredClone(lowered.flow);
  row.enabled = lowered.flow.enabled;
  row.revision += 1;
  return { ok: true, id: flowRouteId(ref), name: row.name, enabled: row.enabled, revision: String(row.revision), warnings: [] };
}

export function demoToggle(ref: FlowRef, enabled: boolean, expectedRevision?: string): ToggleReply {
  const row = ref.layer === 'automation' ? rows.get(ref.id) : undefined;
  if (!row) return { ok: false, reason: 'not_found' };
  if (expectedRevision !== undefined && Number(expectedRevision) !== row.revision) return { ok: false, reason: 'conflict' };
  row.enabled = enabled;
  row.revision += 1;
  return { ok: true, enabled, revision: String(row.revision), warnings: [] };
}

export function demoDelete(ref: FlowRef): DeleteReply {
  return ref.layer === 'automation' && rows.delete(ref.id) ? { ok: true } : { ok: false, reason: 'not_found' };
}

export function demoExportRows(): FlowExportRow[] {
  return [...rows].flatMap(([id, row]) => {
    const lowered = flowToDefinition(row.flow);
    return lowered.ok ? [{ id, name: row.name, definition: lowered.value }] : [];
  });
}

const DEMO_REWARDS: ChoiceOption[] = [
  { value: 'hydrate', label: 'Hydrate (demo reward)' },
  { value: 'song', label: 'Song request (demo reward)' }
];

const DEMO_COUNTERS: ChoiceOption[] = [
  { value: 'deaths', label: 'deaths' },
  { value: 'hugs', label: 'hugs' }
];

const demoScene = (id: number, label: string): ChoiceOption => ({
  value: `lightScene:${id}`,
  label: `${label} (demo scene)`,
  meta: { sceneType: 'devices.capabilities.dynamic_scene', sceneInstance: 'lightScene', sceneValue: String(id) }
});

const DEMO_SCENES: Record<string, ChoiceOption[]> = {
  [DEMO_STRIP.device]: [demoScene(101, 'Sunset glow'), demoScene(102, 'Aurora'), demoScene(103, 'Party')],
  '11:22:33:44:55:66': []
};

export type DemoChoiceMode = 'empty' | 'error' | null;

export const demoChoiceMode = (value: string | null): DemoChoiceMode => (value === 'empty' || value === 'error' ? value : null);

function demoList(options: ChoiceOption[], mode: DemoChoiceMode): ChoiceList {
  if (mode === 'error') return unavailable();
  return { options: mode === 'empty' ? [] : options };
}

export function demoChoices(mode: DemoChoiceMode) {
  return {
    rewards: demoList(DEMO_REWARDS, mode),
    counters: demoList(DEMO_COUNTERS, mode)
  };
}

export function demoScenes(device: string): ChoiceList {
  return device in DEMO_SCENES ? { options: DEMO_SCENES[device]! } : unavailable();
}
