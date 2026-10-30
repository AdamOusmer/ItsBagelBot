// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import brandTokens from '@bagel/ui/styles/brand.css?raw';
import { parts } from '@luzir/bolota';

function token(name: string): string {
    const value = brandTokens.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{3,8})\\s*;`))?.[1];
    if (!value) throw new Error(`@bagel/ui brand.css has no color token --${name}`);
    return value;
}

export type BolotaTone = 'green' | 'tan' | 'pale';

const PALETTES: Record<BolotaTone, { head: string; eye: string }> = {
    green: { head: token('bb-green-glow'), eye: token('bb-black') },
    tan: { head: token('bb-tan-light'), eye: token('bb-black') },
    pale: { head: token('bb-tan-pale'), eye: token('bb-black') },
};

export interface StaticBolota {
    name: string;
    size: number;
    head: string;
    eye: string;
    inner: string;
}

export function staticBolota(name: string, size: number, tone: BolotaTone): StaticBolota {
    const palette = PALETTES[tone];
    return { name, size, head: palette.head, eye: palette.eye, inner: parts(name, { size, palette, background: false }).inner };
}
