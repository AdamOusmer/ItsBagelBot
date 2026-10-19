// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Tick = (now: number) => boolean | void;

const subscribers = new Set<Tick>();

const waking = new Set<Tick>();

let frame = 0;

let running = false;

function cancel(): void {
    if (!frame) return;
    cancelAnimationFrame(frame);
    frame = 0;
}

function schedule(): void {
    if (frame || running) return;
    if (waking.size === 0) return;
    if (typeof document !== 'undefined' && document.hidden) return;
    frame = requestAnimationFrame(run);
}

function runTick(tick: Tick, now: number): void {
    try {
        if (tick(now) === false) waking.delete(tick);
    } catch (error) {
        queueMicrotask(() => {
            throw error;
        });
    }
}

function run(now: number): void {
    frame = 0;
    running = true;
    try {
        for (const tick of Array.from(subscribers)) {
            if (waking.has(tick)) runTick(tick, now);
        }
    } finally {
        running = false;
        schedule();
    }
}

export function subscribe(tick: Tick): () => void {
    subscribers.add(tick);
    waking.add(tick);
    schedule();

    return () => {
        subscribers.delete(tick);
        waking.delete(tick);
        if (waking.size === 0) cancel();
    };
}

function wakeAll(): void {
    for (const subscriber of subscribers) waking.add(subscriber);
}

export function wake(tick?: Tick): void {
    if (!tick) wakeAll();
    else if (subscribers.has(tick)) waking.add(tick);
    schedule();
}

export function isRunning(): boolean {
    return frame !== 0;
}

if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', () => {
        if (document.hidden) cancel();
        else schedule();
    });
}
