// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { fieldSearchText, groupLabel, stepKeywords, stepLabel, stepSummary } from './copy';
import type { Translate } from './messages';
import { LIBRARY_SPECS, STEP_GROUPS, type FlowKind, type NodeSpec, type StepGroup } from './model';

type SearchText = string;

export type StepSection = { key: string; specs: NodeSpec[] } & (
  | { kind: FlowKind; group: StepGroup; label: string }
  | { kind: undefined; group: undefined; label: undefined }
);

const KINDS: readonly FlowKind[] = ['trigger', 'reply', 'condition'];

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
  const fields = [label, words(stepKeywords(t, spec)), words(stepSummary(t, spec)), words(fieldSearchText(t, spec))];
  const scores = query.map((word) => wordScore(word, fields));
  if (scores.includes(0)) return 0;
  const base = query.length + 1;
  const relevance = scores.reduce((total, value) => total + base ** value, 0);
  return label.join(' ').startsWith(query.join(' ')) ? relevance + base ** (fields.length + 1) : relevance;
}

function rankedSteps(t: Translate, tokens: readonly SearchText[], kinds: readonly FlowKind[]): NodeSpec[] {
  const pool = LIBRARY_SPECS.filter((spec) => kinds.includes(spec.kind));
  return pool.map((spec) => ({ spec, score: score(t, spec, tokens) }))
    .filter((result) => result.score > 0)
    .sort((left, right) => right.score - left.score)
    .map((result) => result.spec);
}

export function stepSections(t: Translate, query: SearchText, kinds: readonly FlowKind[] = KINDS): StepSection[] {
  if (!query.trim()) {
    return [...new Set(kinds)].flatMap((kind) => STEP_GROUPS.map((group) => ({
      key: `${kind}.${group}`,
      kind,
      group,
      label: groupLabel(t, group),
      specs: LIBRARY_SPECS.filter((spec) => spec.kind === kind && spec.group === group)
    }))).filter((section) => section.specs.length > 0);
  }
  const tokens = words(query);
  if (tokens.length === 0) return [];
  const specs = rankedSteps(t, tokens, kinds);
  return specs.length === 0 ? [] : [{ key: 'search', kind: undefined, group: undefined, label: undefined, specs }];
}

export function searchSteps(t: Translate, query: SearchText, kinds?: readonly FlowKind[]): NodeSpec[] {
  return stepSections(t, query, kinds).flatMap((section) => section.specs);
}
