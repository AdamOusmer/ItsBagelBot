<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Deterministic creature avatar: the same display name always renders the
  // same face, in every app and every runtime. This is the only place the
  // console touches @luzir/bolota. Everything else goes through this props
  // surface so the seeding rule and the motion budget stay in one file.
  //
  // Two halves, deliberately: `parts()` is pure and SSR-safe, so the static
  // pose is what the server sends and what first paint shows. The live engine
  // is a browser-only upgrade, dynamically imported so it never enters the SSR
  // graph and never ships to a client that will not animate.
  import { parts } from '@luzir/bolota';
  import type { EngineHandle } from '@luzir/bolota/engine';

  let {
    name,
    size = 30,
    active = false,
    cycle = true,
    motionState = 'wander',
    follow = false,
    expression = null,
    sequence = null,
    sequenceKey = 0,
    gate = false,
    title = '',
    class: klass = ''
  }: {
    name: string;
    size?: number;
    // Parent-driven: mount the live engine while true, tear it down when false.
    // Hover on a chip, "menu is open" for a whole list, "step is on screen" for
    // the onboarding guide.
    active?: boolean;
    cycle?: boolean;
    // Never 'idle': idle is deliberately still (dead-ahead gaze, no drift), so
    // it reads as a frozen portrait. 'wander' is the living resting face.
    motionState?: string;
    // Track the pointer across the whole page. Only useful for a large blob
    // that the eye actually follows, so it stays off by default.
    follow?: boolean;
    // Overrides the cycle with one held expression while set. Clearing it
    // hands the face back to the rotation.
    expression?: string | null;
    // A named one-shot from @luzir/bolota/sequences ('entrance', 'burst',
    // 'orbit', 'comet'), played whenever `sequenceKey` changes. Use it for the
    // handoff between two resting positions: a bare state swap snaps, a
    // sequence carries the blob through the change.
    sequence?: 'entrance' | 'burst' | 'orbit' | 'comet' | null;
    // Any value that changes when the sequence should replay. Passing the same
    // name twice is otherwise a no-op, so this is what re-arms it.
    sequenceKey?: unknown;
    // Hold the engine back until the avatar is actually on screen. For long
    // lists only: a moving or transformed host can read as off-screen at the
    // moment it is observed, which would strand the engine unmounted.
    gate?: boolean;
    title?: string;
    class?: string;
  } = $props();

  // Positive and neutral only. The library also ships angry, sad, scared,
  // suspicious, unimpressed and confused; none of them belong on a face
  // representing the signed-in user.
  const POOL = [
    'wander',
    'attentive',
    'surprised',
    'excited',
    'happy',
    'laughing',
    'curious',
    'proud',
    'shy',
    'sleepy',
    'love'
  ];
  const CYCLE_MS = 2600;

  const label = $derived(title || name);
  const pose = $derived(parts(name, { size, background: false, title: label }));
  // `bg` is dropped on purpose: the caller owns the plate behind the blob (the
  // house gradient), so the library never draws one of its own.
  const styleVars = $derived(
    pose.vars
      ? Object.entries(pose.vars)
          .map(([k, v]) => `${k}:${v}`)
          .join(';')
      : undefined
  );

  // Rotation offset, so a dropdown full of avatars does not pulse in lockstep.
  // Seeded by name, so the same person still starts on the same expression.
  const seedOffset = $derived(hash(name) % POOL.length);
  function hash(s: string): number {
    let h = 5381;
    for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0;
    return Math.abs(h);
  }

  let el = $state<SVGSVGElement | null>(null);
  let handle: EngineHandle | null = null;
  // Reactive mirror of "an engine is mounted", so the expression effect below
  // can wake up without the mount effect depending on its own write.
  let ready = $state(false);
  let timer: ReturnType<typeof setInterval> | null = null;
  let step = 0;
  let visible = $state(true);

  // One shared fetch of the engine chunk no matter how many avatars mount at
  // once. It is ~17 KB gzipped and entirely separate from the static renderer.
  let enginePromise: Promise<typeof import('@luzir/bolota/engine')> | null = null;
  const loadEngine = () => (enginePromise ??= import('@luzir/bolota/engine'));
  let sequencePromise: Promise<typeof import('@luzir/bolota/sequences')> | null = null;
  const loadSequences = () => (sequencePromise ??= import('@luzir/bolota/sequences'));
  // The library does not publish a sequence's duration, so the return to the
  // resting loop is timed rather than awaited. Long enough to read as one
  // continuous move, short enough that the blob never strands mid-flourish.
  const SEQUENCE_MS = 1200;
  let settle: ReturnType<typeof setTimeout> | null = null;

  const reducedMotion = () =>
    typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;

  function tickExpression() {
    if (!handle) return;
    handle.setExpression(POOL[(seedOffset + step++) % POOL.length]);
  }

  function startCycle() {
    if (timer || !cycle || !handle) return;
    tickExpression();
    timer = setInterval(tickExpression, CYCLE_MS);
  }

  function stopCycle() {
    if (timer) clearInterval(timer);
    timer = null;
  }

  async function startEngine() {
    if (handle || !el || reducedMotion()) return;
    const { mountEngine } = await loadEngine();
    // `active` can have flipped back off while the chunk was in flight.
    if (!el || handle) return;
    const h = mountEngine(el, name, { size, background: false });
    h.loop(motionState);
    if (follow) h.follow('window');
    step = 0;
    handle = h;
    ready = true;
  }

  function stopEngine() {
    stopCycle();
    if (settle) clearTimeout(settle);
    settle = null;
    ready = false;
    if (!handle) return;
    handle.destroy();
    handle = null;
    // destroy() leaves whatever the engine last drew; put the seeded static
    // pose back so the avatar lands on exactly its first-paint face.
    if (el) el.innerHTML = pose.inner;
  }

  // Off-screen avatars never mount an engine: the budget is roughly 20 at
  // 60fps, and a long dashboard roster would blow through it on its own.
  $effect(() => {
    if (!gate || !el || typeof IntersectionObserver !== 'function') return;
    const io = new IntersectionObserver((entries) => (visible = entries[0].isIntersecting));
    io.observe(el);
    return () => {
      io.disconnect();
      visible = true;
    };
  });

  $effect(() => {
    if (active && visible) startEngine();
    else stopEngine();
    return stopEngine;
  });

  // One-shot transition, replayed whenever the caller changes `sequenceKey`.
  async function runTransition(nameOfSequence: 'entrance' | 'burst' | 'orbit' | 'comet') {
    const { runSequence } = await loadSequences();
    if (!handle) return;
    runSequence(handle, nameOfSequence);
    if (settle) clearTimeout(settle);
    settle = setTimeout(() => handle?.loop(motionState), SEQUENCE_MS);
  }

  $effect(() => {
    sequenceKey;
    if (!ready || !sequence) return;
    runTransition(sequence);
  });

  // A held expression (hover reactions, for one) outranks the rotation: pause
  // the timer while it is set so the two never fight over the same face.
  $effect(() => {
    if (!ready || !handle) return;
    if (expression) {
      stopCycle();
      handle.setExpression(expression);
      return;
    }
    // Clearing the prop has to clear the held face too: the engine keeps the
    // last expression until told otherwise, so without this the blob would
    // stay on its reaction forever once the pointer left.
    handle.setExpression(null);
    startCycle();
  });
</script>

<!-- Only `inner` goes through the html sink; class and custom properties are
     wired as real attributes. That seam is the library's documented contract
     for framework adapters. -->
<svg
  bind:this={el}
  class="bolota {pose.cls ?? ''} {klass}"
  style={styleVars}
  viewBox="0 0 100 100"
  width={size}
  height={size}
  role="img"
  aria-label={label}
>
  {@html pose.inner}
</svg>

<style>
  .bolota { display: block; overflow: visible; }
</style>
