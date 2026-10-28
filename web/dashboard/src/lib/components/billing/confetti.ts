// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type ConfettiPiece = {
  tx: number;
  peak: number;
  fall: number;
  rot: number;
  delay: number;
  dur: number;
  color: string;
  w: number;
  h: number;
};

const CONFETTI_COLORS = ['#d8bf91', '#eadbbd', '#62a593', '#f4f4f5'];
const PIECE_COUNT = 90;

export function confettiPieces(viewportWidth: number, viewportHeight: number, originY: number): ConfettiPiece[] {
  const rise = viewportHeight * 0.28;
  const toFloor = viewportHeight - originY;
  return Array.from({ length: PIECE_COUNT }, () => {
    const angle = (-170 + Math.random() * 160) * (Math.PI / 180);
    const power = 0.55 + Math.random() * 0.75;
    return {
      tx: Math.round(Math.cos(angle) * viewportWidth * 0.42 * power),
      peak: Math.round(Math.abs(Math.sin(angle)) * rise * power),
      fall: Math.round(toFloor + 120 + Math.random() * 200),
      rot: Math.round((Math.random() - 0.5) * 720),
      delay: Math.round(Math.random() * 320),
      dur: Math.round(2600 + Math.random() * 1600),
      color: CONFETTI_COLORS[Math.floor(Math.random() * CONFETTI_COLORS.length)],
      w: 6 + Math.round(Math.random() * 6),
      h: 3 + Math.round(Math.random() * 4)
    };
  });
}
