// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { FlowPreview } from './thumb';
import { flowRouteId, type FlowRef } from './ref';

export type FlowSection = 'commands' | 'alerts' | 'rewards' | 'automations';

export type FlowCard = {
  id: string;
  ref: FlowRef;
  name: string;
  section: FlowSection;
  description: string;
  when: string;
  enabled: boolean;
  revision: string;
  steps: number;
  preview: FlowPreview;
  deletable: boolean;
};

export type CardSource = { ref: FlowRef; name: string; enabled: boolean; revision: string; preview: string | null };

const EMPTY_PREVIEW: FlowPreview = { n: [], e: [] };

const SECTION_OF: Record<FlowRef['layer'], FlowSection> = { command: 'commands', alert: 'alerts', automation: 'automations' };

function readPreview(text: string | null): FlowPreview {
  if (!text) return EMPTY_PREVIEW;
  try {
    const parsed = JSON.parse(text) as Partial<FlowPreview>;
    return Array.isArray(parsed.n) && Array.isArray(parsed.e) ? { n: parsed.n, e: parsed.e } : EMPTY_PREVIEW;
  } catch {
    return EMPTY_PREVIEW;
  }
}

export function cardFromSummary(source: CardSource): FlowCard {
  const preview = readPreview(source.preview);
  return {
    id: flowRouteId(source.ref),
    ref: source.ref,
    name: source.name,
    section: SECTION_OF[source.ref.layer],
    description: '',
    when: '',
    enabled: source.enabled,
    revision: source.revision,
    steps: preview.n.length,
    preview,
    deletable: source.ref.layer !== 'alert'
  };
}
