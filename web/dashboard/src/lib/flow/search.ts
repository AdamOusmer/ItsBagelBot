// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { stepKeywords, stepLabel, stepSummary } from './copy';
import type { Translate } from './messages';
import { NODE_SPECS, type FlowKind, type NodeSpec, type StepGroup } from './model';

type SearchText = string;

const KINDS: readonly FlowKind[] = ['trigger', 'reply', 'condition'];
const GROUPS: readonly StepGroup[] = ['twitch', 'modules', 'basics'];

const words = (value: SearchText): SearchText[] => value.toLowerCase().normalize('NFD').replace(/\p{M}/gu, '').match(/[\p{L}\p{N}]+/gu) ?? [];

function oneEdit(left: SearchText, right: SearchText): boolean {
  if (Math.abs(left.length - right.length) > 1) return false;
  const at = Array.from(left).findIndex((letter, index) => letter !== right[index]);
  if (at < 0) return true;
  if (left.length > right.length) return left.slice(at + 1) === right.slice(at);
  if (right.length > left.length) return left.slice(at) === right.slice(at + 1);
  if (left.slice(at + 1) === right.slice(at + 1)) return true;
  return left.slice(at, at + 2) === right.slice(at, at + 2).split('').reverse().join('') && left.slice(at + 2) === right.slice(at + 2);
}

const matches = (query: SearchText, word: SearchText): boolean => word.startsWith(query) || (query.length >= 4 && oneEdit(query, word));

function wordScore(query: SearchText, fields: readonly SearchText[][]): number {
  const index = fields.findIndex((field) => field.some((word) => matches(query, word)));
  return index < 0 ? 0 : fields.length - index;
}

function score(t: Translate, spec: NodeSpec, query: readonly SearchText[]): number {
  const label = words(stepLabel(t, spec));
  const fields = [label, words(stepKeywords(t, spec)), words(stepSummary(t, spec))];
  const scores = query.map((word) => wordScore(word, fields));
  if (scores.includes(0)) return 0;
  const base = query.length + 1;
  const relevance = scores.reduce((total, value) => total + base ** value, 0);
  return label.join(' ').startsWith(query.join(' ')) ? relevance + base ** 4 : relevance;
}

export function searchSteps(t: Translate, query: string, kinds?: readonly FlowKind[]): NodeSpec[] {
  const activeKinds = kinds ?? KINDS;
  const pool = NODE_SPECS.filter((spec) => activeKinds.includes(spec.kind));
  const tokens = words(query);
  if (tokens.length === 0) return activeKinds.flatMap((kind) => GROUPS.flatMap((group) => pool.filter((spec) => spec.kind === kind && spec.group === group)));
  return pool.map((spec) => ({ spec, score: score(t, spec, tokens) }))
    .filter((result) => result.score > 0)
    .sort((left, right) => right.score - left.score)
    .map((result) => result.spec);
}
