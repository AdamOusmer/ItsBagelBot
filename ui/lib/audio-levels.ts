// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type AudioLevels = {
    level: number;
    brightness: number;
};

const QUIET = 0.02;
const VOICED = 0.03;
const BRIGHT_FROM_HZ = 3800;
const BRIGHT_SPAN_HZ = 1600;

const follow = (current: number, target: number) => current + (target - current) * (target > current ? 0.55 : 0.09);
const clamp01 = (value: number) => Math.min(1, Math.max(0, value));

function rms(wave: Uint8Array): number {
    let sum = 0;
    for (const sample of wave) sum += ((sample - 128) / 128) ** 2;
    return Math.sqrt(sum / wave.length);
}

function centroidHz(bins: Uint8Array, sampleRate: number): number {
    const hzPerBin = sampleRate / 2 / bins.length;
    let weighted = 0;
    let mass = 0;
    for (let i = 1; i < bins.length; i++) {
        weighted += bins[i] * i * hzPerBin;
        mass += bins[i];
    }
    return mass > 0 ? weighted / mass : 0;
}

export function createAudioLevels(): (analyser: AnalyserNode | null) => AudioLevels {
    const state: AudioLevels = { level: 0, brightness: 0 };
    let bins = new Uint8Array(0);
    let wave = new Uint8Array(0);
    let peak = 0.2;

    return (analyser) => {
        if (!analyser) {
            state.level = follow(state.level, 0);
            return state;
        }
        if (bins.length !== analyser.frequencyBinCount) {
            bins = new Uint8Array(analyser.frequencyBinCount);
            wave = new Uint8Array(analyser.fftSize);
        }
        analyser.getByteFrequencyData(bins);
        analyser.getByteTimeDomainData(wave);
        const loud = rms(wave);
        peak = Math.max(peak * 0.997, loud, 0.08);
        state.level = follow(state.level, loud > QUIET ? Math.min(1, loud / peak) : 0);
        if (loud > VOICED) {
            const bright = clamp01((centroidHz(bins, analyser.context.sampleRate) - BRIGHT_FROM_HZ) / BRIGHT_SPAN_HZ);
            state.brightness += (bright - state.brightness) * 0.1;
        }
        return state;
    };
}
