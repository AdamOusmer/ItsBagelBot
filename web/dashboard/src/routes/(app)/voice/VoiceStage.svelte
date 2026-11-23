<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { usageTone, VOICE_TOKENS_PER_SECOND } from '@bagel/kit/voice-plan';
  import AudioOrb from '@bagel/ui/svelte/AudioOrb.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { VoiceAllowance } from '#lib/server/services/voice';
  import VoiceWheel from './VoiceWheel.svelte';

  let { allowance, voice, onVoice }: {
    allowance: VoiceAllowance;
    voice: string;
    onVoice: (voice: string) => void;
  } = $props();

  const { t, locale } = getI18n();
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });
  const numbers = new Intl.NumberFormat(locale);

  function duration(tokens: number) {
    const total = Math.floor(tokens / VOICE_TOKENS_PER_SECOND);
    const minutes = t('common.duration.minutes', { n: numbers.format(Math.floor(total / 60)) });
    if (total >= 60 && total % 60 === 0) return minutes;
    return `${minutes} ${t('common.duration.seconds', { n: numbers.format(total % 60) })}`;
  }

  const date = $derived(dates.format(new Date(allowance.resets_at)));
  const usage = $derived(t('voice.usage', { used: duration(allowance.used_tokens), limit: duration(allowance.limit_tokens) }));
  const fraction = $derived(allowance.limit_tokens > 0 ? allowance.used_tokens / allowance.limit_tokens : 0);
  const exhausted = $derived(usageTone(allowance.used_tokens, allowance.limit_tokens) === 'inactive');
  const footnote = $derived(exhausted
    ? t('voice.exhausted', { date })
    : t('voice.remaining', { time: duration(allowance.remaining_tokens) }));

  let audio = $state<HTMLAudioElement>();
  let analyser = $state<AnalyserNode | null>(null);
  let playing = $state<string | null>(null);

  function connect(element: HTMLAudioElement) {
    if (analyser) return analyser;
    const context = new AudioContext();
    const node = context.createAnalyser();
    node.fftSize = 2048;
    node.smoothingTimeConstant = 0.75;
    context.createMediaElementSource(element).connect(node);
    node.connect(context.destination);
    analyser = node;
    return node;
  }

  function play(next: string) {
    if (!audio) return;
    void (connect(audio).context as AudioContext).resume();
    if (playing === next) {
      audio.pause();
      return;
    }
    audio.src = `/voice-previews/${locale}/${next}.mp3`;
    playing = next;
    void audio.play().catch(() => (playing = null));
  }
</script>

<audio bind:this={audio} preload="none" onpause={() => (playing = null)} onended={() => (playing = null)}></audio>

<div class="usage">
  <div class="usage-line">
    <Text as="span" size="sm"><bb-fit><span data-fit="">{usage}</span></bb-fit></Text>
    <Text as="span" size="xs" tone="muted"><bb-fit><span data-fit="">{t('voice.resets', { date })}</span></bb-fit></Text>
  </div>
  <ProgressBar inset gradient tone="warning" label={t('voice.usageTitle')} aria-valuetext={usage} value={fraction} />
  <Text as="span" size="xs" tone="muted"><bb-fit><span data-fit="">{footnote}</span></bb-fit></Text>
</div>
<div class="orb-slot"><div class="orb-fit"><AudioOrb {analyser} /></div></div>
<VoiceWheel {voice} {playing} onSelect={onVoice} onPlay={play} />

<style>
  .usage {
    --progress-from: var(--bb-green-glow);
    display: grid;
    gap: var(--bb-space-2);
    min-inline-size: 0;
    font-variant-numeric: tabular-nums;
  }
  .usage-line {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, auto);
    align-items: baseline;
    gap: var(--bb-space-3);
  }
  .orb-slot { min-block-size: 0; container-type: size; display: grid; place-items: center; }
  .orb-fit { inline-size: min(100cqw, 100cqh); }
  @media (max-width: 900px) {
    .orb-slot { container-type: normal; }
    .orb-fit { inline-size: min(320px, 80vw); }
  }
</style>
