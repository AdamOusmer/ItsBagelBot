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
    class?: string;
  };

  let { analyser, class: className = '', ...rest }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  type Rgba = [number, number, number, number];
  const REST_TOKENS = ['--bb-tan-darkest', '--bb-tan-mid', '--bb-tan-deep', '--bb-tan'];
  const BRIGHT_TOKENS = ['--bb-green', '--bb-green-light', '--bb-green-glow', '--bb-tan-deep'];

  let host = $state<HTMLDivElement>();

  const mix = (a: Rgba, b: Rgba, t: number): Rgba => [0, 1, 2, 3].map((i) => a[i] + (b[i] - a[i]) * t) as Rgba;

  onMount(() => {
    if (!host) return;
    const styles = getComputedStyle(host);
    const read = (tokens: string[]) => tokens.map((token) => getShaderColorFromString(styles.getPropertyValue(token).trim()));
    const rest = read(REST_TOKENS);
    const bright = read(BRIGHT_TOKENS);
    const still = prefersReducedMotion();
    const mount = new ShaderMount(host, meshGradientFragmentShader, {
      u_colors: rest, u_colorsCount: rest.length, u_distortion: 0.6, u_swirl: 0.2, u_grainMixer: 0, u_grainOverlay: 0.08,
      u_fit: ShaderFitOptions.cover, u_scale: 1, u_rotation: 0, u_originX: defaultObjectSizing.originX,
      u_originY: defaultObjectSizing.originY, u_offsetX: 0, u_offsetY: 0, u_worldWidth: 0, u_worldHeight: 0
    }, undefined, still ? 0 : 0.4);
    const listen = createAudioLevels();
    let drive = 0;
    let brightness = 0;
    let raf = 0;
    const tick = () => {
      const heard = listen(analyser);
      drive += (heard.level - drive) * (heard.level > drive ? 0.08 : 0.04);
      brightness += (heard.brightness - brightness) * 0.08;
      mount.setUniforms({
        u_colors: rest.map((color, i) => mix(color, bright[i], brightness)),
        u_distortion: 0.6 + drive * 0.35,
        u_swirl: 0.2 + drive * 0.7
      });
      if (!still) mount.setSpeed(0.4 + drive * 1.6);
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
