<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { DEFAULT_VOICE, MAX_CLIP_SECONDS, usageTone, VOICE_TOKENS_PER_SECOND } from '@bagel/kit/voice-plan';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { VoiceAllowance, VoiceSettings } from '#lib/server/services/voice';

  let { allowance, settings }: { allowance: VoiceAllowance; settings?: VoiceSettings } = $props();
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
  const exhausted = $derived(tone === 'inactive');
  const facts = $derived([
    { label: t('voice.choose'), value: settings?.voice || DEFAULT_VOICE },
    { label: t('voice.clipLimit'), value: t('common.duration.seconds', { n: numbers.format(settings?.max_seconds ?? MAX_CLIP_SECONDS) }) }
  ]);
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
    <div class="meter-status">
      <div class="meter-details" class:visible={!exhausted} aria-hidden={exhausted}>
        <div class="bb-fit-box">
          <Text as="div" size="sm" tone="muted"><bb-fit><span data-fit="">
            {t('voice.remaining', { time: duration(allowance.remaining_tokens) })}
          </span></bb-fit></Text>
        </div>
        <div class="bb-fit-box">
          <Text as="div" size="sm" tone="muted"><bb-fit><span data-fit="">{t('voice.resets', { date })}</span></bb-fit></Text>
        </div>
      </div>
      <div class="meter-note bb-fit-box" class:visible={exhausted} aria-hidden={!exhausted}>
        <Text as="div" size="sm" tone="muted"><bb-fit mode="block">{t('voice.exhausted', { date })}</bb-fit></Text>
      </div>
    </div>
  </div>
  <dl class="meter-facts">
    {#each facts as { label, value } (label)}
      <div class="fact">
        <dt class="fact-label bb-fit-box"><bb-fit><span data-fit="">{label}</span></bb-fit></dt>
        <dd class="fact-value">{value}</dd>
      </div>
    {/each}
  </dl>
</Card>

<style>
  .card-head, .meter-details, .fact {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: center;
    gap: var(--bb-space-2);
  }
  .card-head { margin-block-end: var(--bb-space-3); }
  .plan { justify-self: end; min-inline-size: 0; max-inline-size: 100%; }
  .meter { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-2); }
  .usage { block-size: 2rem; align-content: center; font-variant-numeric: tabular-nums; }
  .meter-status { display: grid; grid-template-columns: minmax(0, 1fr); }
  .meter-details, .meter-note {
    grid-area: 1 / 1;
    block-size: 2rem;
    align-content: center;
    opacity: 0;
  }
  .meter-details > :last-child { text-align: end; }
  .meter-details.visible, .meter-note.visible { opacity: 1; }
  .meter-facts {
    display: grid;
    gap: var(--bb-space-2);
    margin: var(--bb-space-4) 0 0;
  }
  .fact { min-block-size: 1.5rem; }
  .fact-label { color: var(--bb-muted); font-size: var(--bb-text-sm); }
  .fact-value { margin: 0; font-variant-numeric: tabular-nums; text-align: end; }
  @media (prefers-reduced-motion: no-preference) {
    .meter-details, .meter-note { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
