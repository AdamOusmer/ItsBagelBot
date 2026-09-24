<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onDestroy } from 'svelte';
  import { prefersReducedMotion as reducedMotion } from '@bagel/ui/lib/motion-query';
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
    sequenceHold = 0,
    sequenceFor = 0,
    gate = false,
    title = '',
    class: klass = ''
  }: {
    name: string;
    size?: number;
    active?: boolean;
    cycle?: boolean;
    motionState?: string;
    follow?: boolean;
    expression?: string | null;
    sequence?: 'entrance' | 'burst' | 'orbit' | 'comet' | null;
    sequenceKey?: unknown;
    sequenceHold?: number;
    sequenceFor?: number;
    gate?: boolean;
    title?: string;
    class?: string;
  } = $props();

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
  const styleVars = $derived(
    pose.vars
      ? Object.entries(pose.vars)
          .map(([k, v]) => `${k}:${v}`)
          .join(';')
      : undefined
  );

  const seedOffset = $derived(hash(name) % POOL.length);
  function hash(s: string): number {
    let h = 5381;
    for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0;
    return Math.abs(h);
  }

  let el = $state<SVGSVGElement | null>(null);
  let handle: EngineHandle | null = null;
  let ready = $state(false);
  let running = false;
  let timer: ReturnType<typeof setInterval> | null = null;
  let step = 0;
  let visible = $state(true);

  let enginePromise: Promise<typeof import('@luzir/bolota/engine')> | null = null;
  const loadEngine = () => (enginePromise ??= import('@luzir/bolota/engine'));
  let sequencePromise: Promise<typeof import('@luzir/bolota/sequences')> | null = null;
  const loadSequences = () => (sequencePromise ??= import('@luzir/bolota/sequences'));

  function tickExpression() {
    if (!handle) return;
    handle.setExpression(POOL[(seedOffset + step++) % POOL.length]);
  }

  function startCycle() {
    if (timer || !cycle || !handle || !running) return;
    tickExpression();
    timer = setInterval(tickExpression, CYCLE_MS);
  }

  function stopCycle() {
    if (timer) clearInterval(timer);
    timer = null;
  }

  let mounting = false;

  async function startEngine() {
    if (handle || mounting || !el || reducedMotion()) return;
    mounting = true;
    const { mountEngine } = await loadEngine();
    if (!el || handle) {
      mounting = false;
      return;
    }
    const h = mountEngine(el, name, { size, background: false });
    h.loop(motionState);
    if (follow) h.follow('window');
    step = 0;
    handle = h;
    mounting = false;
    running = true;
    ready = true;
  }

  function pauseEngine() {
    stopCycle();
    running = false;
    handle?.stop();
  }

  function resumeEngine() {
    running = true;
    if (!handle) {
      startEngine();
      return;
    }
    handle.play(motionState, { loop: true });
    if (cycle) startCycle();
  }

  function destroyEngine() {
    stopCycle();
    mounting = false;
    running = false;
    ready = false;
    if (!handle) return;
    handle.destroy();
    handle = null;
  }

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
    if (!visible) destroyEngine();
    else if (active) resumeEngine();
    else pauseEngine();
  });

  onDestroy(destroyEngine);

  async function runTransition(nameOfSequence: 'entrance' | 'burst' | 'orbit' | 'comet') {
    const { runSequence } = await loadSequences();
    if (!handle) return;
    runSequence(handle, nameOfSequence, {
      for: sequenceFor / 1000,
      hold: sequenceHold / 1000,
      rest: motionState
    });
  }

  $effect(() => {
    sequenceKey;
    if (!ready || !sequence) return;
    runTransition(sequence);
  });

  $effect(() => {
    if (!ready || !handle || !running) return;
    if (expression) {
      stopCycle();
      handle.setExpression(expression);
      return;
    }
    handle.setExpression(null);
    startCycle();
  });
</script>

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
  <g style:display={ready ? 'none' : null}>{@html pose.inner}</g>
</svg>

<style>
  .bolota { display: block; overflow: visible; }
</style>
