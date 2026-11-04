// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type Pt = { x: number; y: number };

export function wirePath(a: Pt, b: Pt, { turn = 28, radius = 14 } = {}): string {
  const dy = b.y - a.y;
  if (Math.abs(dy) < 1) return `M${a.x},${a.y} L${b.x},${b.y}`;
  const across = Math.sign(b.x - a.x) || 1;
  const down = Math.sign(dy);
  const reach = Math.min(turn, Math.abs(b.x - a.x) / 2);
  const turnX = a.x + across * reach;
  const r = Math.min(radius, Math.abs(dy) / 2, reach);
  return [
    `M${a.x},${a.y}`,
    `H${turnX - across * r}`,
    `Q${turnX},${a.y} ${turnX},${a.y + down * r}`,
    `V${b.y - down * r}`,
    `Q${turnX},${b.y} ${turnX + across * r},${b.y}`,
    `H${b.x}`
  ].join(' ');
}
