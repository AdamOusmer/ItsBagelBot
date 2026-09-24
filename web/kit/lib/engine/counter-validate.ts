// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const COUNTER_NAME_MAX = 64;

export function bumpCounterProblem(name: string): string | undefined {
  const stripped = name.replace(/^!/, '');
  if (!stripped) return undefined;
  if (stripped.length > COUNTER_NAME_MAX) return `Counter name must be at most ${COUNTER_NAME_MAX} characters.`;
  if (/\s/.test(stripped)) return 'Counter name cannot contain spaces.';
  if (stripped.includes(':')) return 'Counter name cannot contain ":".';
  return undefined;
}
