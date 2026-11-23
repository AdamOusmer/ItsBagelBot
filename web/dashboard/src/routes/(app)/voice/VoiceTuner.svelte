<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import AudioOrb from '@bagel/ui/svelte/AudioOrb.svelte';
  import VoiceWheel from './VoiceWheel.svelte';

  let { voice, onVoice, orbSize, autoplay = false, lead, tail }: {
    voice: string;
    onVoice: (voice: string) => void;
    orbSize?: string;
    autoplay?: boolean;
    lead?: Snippet;
    tail?: Snippet;
  } = $props();

  const { locale } = getI18n();

  let audio = $state<HTMLAudioElement>();
  let analyser = $state<AnalyserNode | null>(null);
  let playing = $state<string | null>(null);

  function connect(element: HTMLAudioElement) {
    const context = new AudioContext();
    const node = context.createAnalyser();
    node.fftSize = 2048;
    node.smoothingTimeConstant = 0.75;
    context.createMediaElementSource(element).connect(node);
    node.connect(context.destination);
    return node;
  }

  function play(next: string) {
    if (!audio) return;
    analyser ??= connect(audio);
    void (analyser.context as AudioContext).resume();
    if (playing === next) {
      audio.pause();
      return;
    }
    audio.src = `/voice-previews/${locale}/${next}.mp3`;
    playing = next;
    void audio.play().catch(() => (playing = null));
  }

  function select(next: string) {
    onVoice(next);
    if (autoplay && playing !== next) play(next);
  }
</script>

<audio bind:this={audio} preload="none" onpause={() => (playing = null)} onended={() => (playing = null)}></audio>

<div class="orb-slot" class:sized={!!orbSize} style:--orb-size={orbSize}>
  <div class="orb-fit"><AudioOrb {analyser} /></div>
</div>
<div class="side">
  {@render lead?.()}
  <VoiceWheel {voice} {playing} onSelect={select} onPlay={play} />
  {@render tail?.()}
</div>

<style>
  .orb-slot { min-block-size: 0; container-type: size; display: grid; place-items: center; }
  .orb-fit { inline-size: min(100cqw, 100cqh); }
  .orb-slot.sized { container-type: normal; }
  .side { display: grid; gap: var(--bb-space-5); min-inline-size: 0; }
  .orb-slot.sized .orb-fit { inline-size: var(--orb-size); }
  @media (max-width: 900px) {
    .orb-slot { container-type: normal; }
    .orb-fit { inline-size: min(320px, 80vw); }
  }
</style>
