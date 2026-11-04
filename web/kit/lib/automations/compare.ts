// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const COMPARE_OPS = ['gte', 'lt', 'eq', 'contains'];

const DECIMAL = /^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$/;
const EDGE_SPACE = /^[ \t\r\n]+|[ \t\r\n]+$/g;

function decimalValue(text: string): number | null {
  const trimmed = text.replace(EDGE_SPACE, '');
  const value = DECIMAL.test(trimmed) ? Number(trimmed) : NaN;
  return Number.isFinite(value) ? value : null;
}

function orderedPasses(op: string, left: string, right: string): boolean {
  const a = decimalValue(left);
  const b = decimalValue(right);
  if (a === null || b === null) return false;
  return op === 'lt' ? a < b : a >= b;
}

// Must agree with ComparePasses in internal/domain/automation/compare.go; testdata/compare_cases.json pins both.
export function comparePasses(op: string, left: string, right: string): boolean {
  switch (op) {
    case 'eq':
      return left === right;
    case 'contains':
      return right !== '' && left.includes(right);
    case 'gte':
    case 'lt':
      return orderedPasses(op, left, right);
    default:
      return false;
  }
}
