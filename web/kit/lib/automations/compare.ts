// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { simpleLower } from './text';
import { CMP_OPS } from './values';

export const COMPARE_OPS = [...CMP_OPS, 'contains', 'starts'];
export const TEXT_COMPARE_OPS = ['eq', 'ne', 'contains', 'starts'];

export const needsSearchValue = (op: unknown): boolean => op === 'contains' || op === 'starts';

const DECIMAL = /^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$/;
const EDGE_SPACE = new Set([' ', '\t', '\r', '\n']);

function trimEdgeSpace(text: string): string {
  let start = 0;
  let end = text.length;
  while (start < end && EDGE_SPACE.has(text[start]!)) start++;
  while (end > start && EDGE_SPACE.has(text[end - 1]!)) end--;
  return text.slice(start, end);
}

function decimalValue(text: string): number | null {
  const trimmed = trimEdgeSpace(text);
  const value = DECIMAL.test(trimmed) ? Number(trimmed) : NaN;
  return Number.isFinite(value) ? value : null;
}

export const isDecimal = (text: string): boolean => decimalValue(text) !== null;

function orderedPasses(op: string, left: string, right: string): boolean {
  const a = decimalValue(left);
  const b = decimalValue(right);
  if (a === null || b === null) return false;
  switch (op) {
    case 'gt': return a > b;
    case 'gte': return a >= b;
    case 'lt': return a < b;
    case 'lte': return a <= b;
    default: return false;
  }
}

// Must agree with ComparePasses in internal/domain/automation/compare.go; testdata/compare_cases.json pins both.
export function comparePasses(op: string, left: string, right: string, ignoreCase = false): boolean {
  if (ignoreCase) {
    left = simpleLower(left);
    right = simpleLower(right);
  }
  switch (op) {
    case 'eq':
      return left === right;
    case 'ne':
      return left !== right;
    case 'contains':
      return right !== '' && left.includes(right);
    case 'starts':
      return right !== '' && left.startsWith(right);
    default:
      return orderedPasses(op, left, right);
  }
}
