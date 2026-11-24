<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import type { SvelteHTMLElements } from 'svelte/elements';
  import {
    defaultObjectSizing, getShaderColorFromString, meshGradientFragmentShader, ShaderFitOptions, ShaderMount
  } from '@paper-design/shaders';
  import '../styles/elements/audio-orb.css';
  import { createAudioLevels } from '../lib/audio-levels';
  import { prefersReducedMotion } from '../lib/motion-query';

  type Own = {
    analyser: AnalyserNode | null;
    energy?: number;
    class?: string;
  };

  let { analyser, energy = 0, class: className = '', ...rest }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  type Rgba = [number, number, number, number];
  const REST_TOKENS = ['--bb-tan-darkest', '--bb-tan-mid', '--bb-tan-deep', '--bb-tan'];
  const BRIGHT_TOKENS = ['--bb-green', '--bb-green-light', '--bb-green-glow', '--bb-tan-deep'];
  const IDLE_TOKENS = ['--bb-green-glow', '--bb-green-light', '--bb-tan', '--bb-tan-light'];

  let host = $state<HTMLDivElement>();

  const mix = (a: Rgba, b: Rgba, t: number): Rgba => [0, 1, 2, 3].map((i) => a[i] + (b[i] - a[i]) * t) as Rgba;
  const lift = ([r, g, b, a]: Rgba, by: number): Rgba => [r * by, g * by, b * by, a];

  onMount(() => {
    if (!host) return;
    const styles = getComputedStyle(host);
    const read = (tokens: string[]) => tokens.map((token) => getShaderColorFromString(styles.getPropertyValue(token).trim()));
    const rest = read(REST_TOKENS);
    const bright = read(BRIGHT_TOKENS);
    const idle = read(IDLE_TOKENS);
    const still = prefersReducedMotion();
    const mount = new ShaderMount(host, meshGradientFragmentShader, {
      u_colors: idle, u_colorsCount: idle.length, u_distortion: 0.2, u_swirl: 0.08, u_grainMixer: 0, u_grainOverlay: 0.08,
      u_fit: ShaderFitOptions.cover, u_scale: 1, u_rotation: 0, u_originX: defaultObjectSizing.originX,
      u_originY: defaultObjectSizing.originY, u_offsetX: 0, u_offsetY: 0, u_worldWidth: 0, u_worldHeight: 0
    }, undefined, 0);
    const listen = createAudioLevels();
    const breathDepth = still ? 0 : 0.07;
    let drive = 0;
    let wake = 0;
    let brightness = 0;
    let raf = 0;
    const tick = (now: number) => {
      const heard = listen(analyser);
      const level = Math.max(heard.level, energy);
      drive += (level - drive) * (level > drive ? 0.1 : 0.05);
      wake += (Math.min(1, drive * 3) - wake) * 0.04;
      brightness += (Math.max(heard.brightness, energy) - brightness) * 0.08;
      const breath = 1 + Math.sin(now / 800) * breathDepth * (1 - wake);
      mount.setUniforms({
        u_colors: rest.map((color, i) => lift(mix(idle[i], mix(color, bright[i], brightness), wake), breath)),
        u_distortion: 0.2 + wake * 0.4 + drive * 0.4,
        u_swirl: 0.08 + wake * 0.12 + drive * 0.8
      });
      if (!still) mount.setSpeed(0.35 + wake * 0.45 + drive * 1.8);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(raf);
      mount.dispose();
    };
  });
</script>

<div class={['bb-audio-orb', className].filter(Boolean).join(' ')} aria-hidden="true" {...rest}>
  <div class="bb-audio-orb__canvas" bind:this={host}></div>
  <div class="bb-audio-orb__shade"></div>
</div>
