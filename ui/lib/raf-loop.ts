// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * One requestAnimationFrame loop for the whole page.
 *
 * Six independent loops existed before this file: the marketing smooth-scroll
 * driver (`script/scroll.js`), the cursor follower, the pointer-parallax system
 * (`script/dom-motion/index.js`), the console's magnetic-hover action and its
 * count-up action (`kit/lib/actions.ts`), and the mote field
 * (`lib/light-field.ts`). On the marketing home page four of them ran at once.
 *
 * That is not a style complaint. Each loop is a separate `requestAnimationFrame`
 * registration, so the browser runs N callbacks per frame instead of one, each
 * reading and writing layout in its own turn. The reads and writes interleave
 * across callbacks, which is how a page with four smooth 60fps effects in
 * isolation gets forced layout in the profiler with all four on. Batching them
 * behind one callback makes the ordering deterministic: every subscriber runs
 * inside the same frame, in subscribe order.
 *
 * Self-suspending, which is the other half of the point. A subscriber that
 * returns `false` has settled and stops being ticked; when the last one settles
 * the loop cancels itself and the page schedules no frames at all until
 * something calls `wake()`. An always-on rAF on an idle page is a measurable
 * battery cost on laptops and the reason the old dom-motion loop grew its own
 * settle timer.
 *
 * WHY `false` MEANS SETTLE, rather than `true` meaning continue: the two
 * biggest subscribers are third-party-shaped tick functions — `lenis.raf(t)`
 * returns void, and the mote field's draw returns void — and a rule of "return
 * nothing, keep running" lets them be passed straight in. Requiring an explicit
 * `true` would mean wrapping both in an arrow that returns it, and the day
 * someone forgets the wrapper the effect silently stops after one frame instead
 * of loudly running forever. The failure mode of the chosen polarity (an effect
 * that forgot to settle) is visible in a profiler; the other one is invisible.
 *
 * Not a class and not per-instance: a second scheduler would defeat the entire
 * purpose, so this module IS the singleton. Import it anywhere.
 */

/**
 * A per-frame callback. Return `false` once the animation has settled and no
 * further frame is wanted; return anything else (including nothing) to be ticked
 * again next frame.
 */
export type Tick = (now: number) => boolean | void;

/** Everything currently subscribed, in subscribe order (Set preserves it). */
const subscribers = new Set<Tick>();

/** The subset that still wants frames. Settled subscribers stay in `subscribers`. */
const waking = new Set<Tick>();

let frame = 0;

function cancel(): void {
    if (!frame) return;
    cancelAnimationFrame(frame);
    frame = 0;
}

function schedule(): void {
    if (frame || waking.size === 0) return;
    // A hidden tab gets no frames from the browser anyway on every engine that
    // matters, but the explicit check is what makes `wake()` from a background
    // timer (a websocket message, a setInterval) not leave a scheduled callback
    // pending for however long the tab stays hidden.
    if (typeof document !== 'undefined' && document.hidden) return;
    frame = requestAnimationFrame(run);
}

function run(now: number): void {
    frame = 0;
    // Snapshot: a tick is allowed to unsubscribe itself, or another subscriber,
    // from inside the loop. Iterating the live Set while it is being mutated
    // skips the neighbour.
    for (const tick of Array.from(waking)) {
        if (!subscribers.has(tick)) {
            waking.delete(tick);
            continue;
        }
        if (tick(now) === false) waking.delete(tick);
    }
    schedule();
}

/**
 * Register `tick` and start ticking it. Returns the unsubscribe; call it from
 * the engine's `dispose()`.
 *
 * The first frame is scheduled immediately, so an engine that only needs one
 * paint can subscribe, draw, and return `false`.
 */
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

/** Un-settle every subscriber. Split out of `wake` to keep it flat. */
function wakeAll(): void {
    for (const subscriber of subscribers) waking.add(subscriber);
}

/**
 * Un-settle a subscriber (or every subscriber, with no argument) and resume the
 * loop.
 *
 * This is what a settled effect is restarted by: the magnetic action calls it
 * on `pointermove`, the parallax system on pointer activity. Waking a subscriber
 * that never settled is free — it is already in the wake set.
 *
 * The no-argument form ticks everything once, including subscribers that had
 * settled and immediately settle again. One extra frame across the page is the
 * deliberate price of not making every caller hold onto its own tick reference
 * just to name itself.
 */
export function wake(tick?: Tick): void {
    if (!tick) wakeAll();
    else if (subscribers.has(tick)) waking.add(tick);
    schedule();
}

/** Whether a frame is currently scheduled. Exists for the unit test. */
export function isRunning(): boolean {
    return frame !== 0;
}

if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', () => {
        if (document.hidden) cancel();
        else schedule();
    });
}
