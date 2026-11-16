// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { variablesFor, type Flow, type FlowNode } from './model';

export function defaultComparison(flow: Flow, node: FlowNode): 'eq' | 'gte' {
  const samples = new Map(variablesFor(flow, node.id).map((variable) => [variable.token, variable.sample]));
  const values = [node.config.left, node.config.right].map((value) => String(value ?? '').trim()).filter(Boolean);
  const text = values.some((value) => !Number.isFinite(Number(value.replace(/\{[^}]+\}/g, (token) => samples.get(token) ?? token))));
  return text ? 'eq' : 'gte';
}
