// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Compare git tags like `v0.1.3-beta` for newest-first changelog order.
 * Core numbers win first. A release without a prerelease suffix outranks
 * the same core with one (`v1.0.0` > `v1.0.0-beta`). Prerelease labels
 * then sort lexicographically (`alpha` < `beta`), which matches how we
 * ship. Same-day `date` values made date sort unstable across 0.1.1–0.1.3.
 */
export function compareVersion(a: string, b: string): number {
  const pa = parseVersion(a);
  const pb = parseVersion(b);
  for (let i = 0; i < 3; i++) {
    if (pa.core[i] !== pb.core[i]) return pa.core[i] - pb.core[i];
  }
  if (pa.pre === null && pb.pre === null) return 0;
  if (pa.pre === null) return 1;
  if (pb.pre === null) return -1;
  return pa.pre < pb.pre ? -1 : pa.pre > pb.pre ? 1 : 0;
}

function parseVersion(tag: string): { core: [number, number, number]; pre: string | null } {
  const stripped = tag.replace(/^v/i, '');
  const dash = stripped.indexOf('-');
  const corePart = dash === -1 ? stripped : stripped.slice(0, dash);
  const pre = dash === -1 ? null : stripped.slice(dash + 1);
  const parts = corePart.split('.').map((n) => {
    const v = Number.parseInt(n, 10);
    return Number.isFinite(v) ? v : 0;
  });
  while (parts.length < 3) parts.push(0);
  return { core: [parts[0], parts[1], parts[2]], pre };
}
