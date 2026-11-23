<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { tick } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { DEFAULT_VOICE } from '@bagel/kit/voice-plan';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import { connectAnalyser } from './audio-analyser';
  import { voiceOptions } from './voice-options';

  let { analyser = $bindable(null) }: { analyser?: AnalyserNode | null } = $props();

  const { t, locale } = getI18n();
  const options = voiceOptions(t);

  let voice = $state<string>(DEFAULT_VOICE);
  let playing = $state(false);
  let audio = $state<HTMLAudioElement>();
  const src = $derived(`/voice-previews/${locale}/${voice}.mp3`);

  function play() {
    if (!audio) return;
    analyser ??= connectAnalyser(audio);
    void (analyser.context as AudioContext).resume();
    audio.currentTime = 0;
    void audio.play().catch(() => (playing = false));
  }

  function toggle() {
    if (playing) audio?.pause();
    else play();
  }

  async function pick(next: string) {
    voice = next;
    await tick();
    play();
  }
</script>

<div class="preview">
  <span class="title">{t('voice.previewTitle')}</span>
  <div class="row">
    <div class="picker">
      <Select label={t('voice.previewVoice')} value={voice} {options} searchable fill
        onchange={(event) => pick(event.currentTarget.value)} />
    </div>
    <IconButton label={playing ? t('voice.previewStop') : t('voice.previewPlay', { voice })} onclick={toggle}>
      <Icon name={playing ? 'stop' : 'play'} />
    </IconButton>
  </div>
  <audio bind:this={audio} {src} preload="none" onplay={() => (playing = true)}
    onpause={() => (playing = false)} onemptied={() => (playing = false)}></audio>
</div>

<style>
  .preview { display: flex; flex-direction: column; gap: 8px; width: min(360px, 100%); }
  .title { font-size: 14px; font-weight: 600; color: rgba(var(--bb-white-rgb), 0.72); }
  .row { display: flex; align-items: center; gap: 8px; }
  .picker { flex: 1; min-width: 0; }
</style>
