<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { usageTone, VOICE_TOKENS_PER_SECOND } from '@bagel/kit/voice-plan';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { VoiceAllowance } from '#lib/server/services/voice';
  import VoiceTuner from './VoiceTuner.svelte';

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
</script>

<div class="usage">
  <div class="usage-line">
    <Text as="span" size="sm"><bb-fit><span data-fit="">{usage}</span></bb-fit></Text>
    <Text as="span" size="xs" tone="muted"><bb-fit><span data-fit="">{t('voice.resets', { date })}</span></bb-fit></Text>
  </div>
  <ProgressBar inset gradient tone="warning" label={t('voice.usageTitle')} aria-valuetext={usage} value={fraction} />
  <Text as="span" size="xs" tone="muted"><bb-fit><span data-fit="">{footnote}</span></bb-fit></Text>
</div>
<VoiceTuner {voice} {onVoice} />

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
</style>
