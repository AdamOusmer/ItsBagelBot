<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { usageTone, VOICE_TOKENS_PER_SECOND } from '@bagel/kit/voice-plan';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { VoiceAllowance } from '#lib/server/services/voice';

  let { allowance }: { allowance: VoiceAllowance } = $props();
  const { t, locale } = getI18n();
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });
  const numbers = new Intl.NumberFormat(locale);
  const plans = {
    free: { label: t('planLabel.free'), tone: 'free' },
    premium: { label: t('voice.planPremium'), tone: 'paid' },
    giveaway: { label: t('voice.planGiveaway'), tone: 'paid' },
    admin_grant: { label: t('voice.planAdmin'), tone: 'paid' },
    vip: { label: t('planLabel.vip'), tone: 'vip' }
  } as const;

  function duration(tokens: number) {
    const total = Math.floor(tokens / VOICE_TOKENS_PER_SECOND);
    const minutes = t('common.duration.minutes', { n: numbers.format(Math.floor(total / 60)) });
    if (total >= 60 && total % 60 === 0) return minutes;
    const seconds = t('common.duration.seconds', { n: numbers.format(total % 60) });
    return `${minutes} ${seconds}`;
  }

  const plan = $derived(plans[allowance.plan]);
  const date = $derived(dates.format(new Date(allowance.resets_at)));
  const tone = $derived(usageTone(allowance.used_tokens, allowance.limit_tokens));
  const fraction = $derived(allowance.limit_tokens > 0 ? allowance.used_tokens / allowance.limit_tokens : 0);
  const usage = $derived(t('voice.usage', { used: duration(allowance.used_tokens), limit: duration(allowance.limit_tokens) }));
</script>

<Card as="section" aria-labelledby="voice-usage">
  <div class="card-head">
    <Heading level={6} as="h2" id="voice-usage">
      <bb-fit><span data-fit="">{t('voice.usageTitle')}</span></bb-fit>
    </Heading>
    <div class="plan">
      <Tag tone={plan.tone} bare contain><bb-fit><span data-fit="">{plan.label}</span></bb-fit></Tag>
    </div>
  </div>
  <div class="meter">
    <div class="usage">
      <Text as="div" size="lg"><bb-fit><span data-fit="">{usage}</span></bb-fit></Text>
    </div>
    <ProgressBar inset {tone} label={t('voice.usageTitle')} aria-valuetext={usage} value={fraction} />
    <div class="meter-details">
      <div class="bb-fit-box">
        <Text as="div" size="sm" tone="muted"><bb-fit><span data-fit="">
          {t('voice.remaining', { time: duration(allowance.remaining_tokens) })}
        </span></bb-fit></Text>
      </div>
      <div class="bb-fit-box">
        <Text as="div" size="sm" tone="muted"><bb-fit><span data-fit="">{t('voice.resets', { date })}</span></bb-fit></Text>
      </div>
    </div>
    <div class="meter-note bb-fit-box" class:exhausted={tone === 'inactive'} aria-hidden={tone !== 'inactive'}>
      <Text as="div" size="sm" tone="muted"><bb-fit mode="block">{t('voice.exhausted', { date })}</bb-fit></Text>
    </div>
  </div>
</Card>

<style>
  .card-head, .meter-details {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: center;
    gap: var(--bb-space-2);
  }
  .card-head { margin-block-end: var(--bb-space-3); }
  .plan { justify-self: end; min-inline-size: 0; max-inline-size: 100%; }
  .meter { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-2); }
  .usage { block-size: 2rem; align-content: center; font-variant-numeric: tabular-nums; }
  .meter-details { block-size: 1.5rem; }
  .meter-details > :last-child { text-align: end; }
  .meter-note { block-size: 2.5rem; align-content: center; opacity: 0; }
  .meter-note.exhausted { opacity: 1; }
</style>
