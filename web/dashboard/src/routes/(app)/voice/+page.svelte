<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { CLOUD_VOICES, DEFAULT_VOICE, VOICE_TOKENS_PER_SECOND } from '@bagel/kit/voice-plan';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import type { ActionData, PageData } from './$types';

  let { data, form }: { data: PageData; form: ActionData } = $props();
  const { t, locale } = getI18n();
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });
  const numbers = new Intl.NumberFormat(locale);
  const options = CLOUD_VOICES.map((voice) => ({ value: voice, label: voice }));
  const plans = {
    free: { label: t('planLabel.free'), tone: 'free' },
    premium: { label: t('voice.planPremium'), tone: 'paid' },
    giveaway: { label: t('voice.planGiveaway'), tone: 'paid' },
    admin_grant: { label: t('voice.planAdmin'), tone: 'paid' },
    vip: { label: t('planLabel.vip'), tone: 'vip' }
  } as const;

  const minutes = (tokens: number) => numbers.format(Math.floor(tokens / (VOICE_TOKENS_PER_SECOND * 60)));
  function remaining(tokens: number): string {
    const seconds = Math.floor(tokens / VOICE_TOKENS_PER_SECOND);
    if (seconds < 60) return t('voice.secondsLeft', { seconds: numbers.format(seconds) });
    return t('voice.minutesLeft', { minutes: minutes(tokens) });
  }

  const allowance = $derived(data.allowance);
  let enabled = $derived(allowance?.enabled ?? false);
  let voice: string = $derived(allowance?.voice ?? DEFAULT_VOICE);
  let saving = $state(false);

  const submit: SubmitFunction = () => {
    saving = true;
    return async ({ update }) => {
      try {
        await update({ reset: false });
      } finally {
        saving = false;
      }
    };
  };
</script>

<svelte:head><title>{t('nav.voice')}</title></svelte:head>

<div class="voicePage">
  <PageHead><bb-fit><span data-fit="">{t('nav.voice')}</span></bb-fit></PageHead>

  {#if data.readOnly}
    <AlertBanner tone="info" role="note">
      <span class="noticeCopy"><bb-fit mode="block">{t('voice.readOnly')}</bb-fit></span>
    </AlertBanner>
  {:else if allowance?.reason === 'inactive' || allowance?.reason === 'banned'}
    <AlertBanner tone="info" role="note">
      <span class="noticeCopy"><bb-fit mode="block">
        {allowance.reason === 'inactive' ? t('voice.inactive') : t('voice.banned')}
      </bb-fit></span>
    </AlertBanner>
  {:else if allowance}
    {@const plan = plans[allowance.plan]}
    {@const hasAllowance = allowance.limit_tokens > 0}
    {@const usage = t('voice.usage', { used: minutes(allowance.used_tokens), limit: minutes(allowance.limit_tokens) })}
    <Card>
      <div class="voicePanel">
        <div class="usageHead">
          <Text as="div" size="lg"><bb-fit><span data-fit="">{usage}</span></bb-fit></Text>
          <Tag tone={plan.tone} contain><bb-fit><span data-fit="">{plan.label}</span></bb-fit></Tag>
        </div>
        <ProgressBar size="sm" label={usage} value={hasAllowance ? allowance.used_tokens / allowance.limit_tokens : 0} />
        <div class="usageDetails">
          <Text size="sm" tone="muted"><bb-fit><span data-fit="">
            {t('voice.resets', { date: dates.format(new Date(allowance.resets_at)) })}
          </span></bb-fit></Text>
          {#if hasAllowance}
            <Text size="sm" tone="muted"><bb-fit><span data-fit="">{remaining(allowance.remaining_tokens)}</span></bb-fit></Text>
          {/if}
        </div>
      </div>
    </Card>

    {#if hasAllowance}
      <Card>
        <form class="voicePanel" method="POST" action="?/save" use:enhance={submit}>
          <SwitchRow control="end" fitCopy bind:checked={enabled} disabled={saving}
            label={t('voice.enable')} hint={t('voice.fallback')} hintId="voice-fallback" />
          <input type="hidden" name="enabled" value={String(enabled)} />
          <div class="voiceField">
            <label for="alert-voice"><bb-fit><span data-fit="">{t('voice.choose')}</span></bb-fit></label>
            <Select id="alert-voice" name="voice" label={t('voice.choose')} bind:value={voice} {options}
              disabled={saving} required fill />
          </div>
          <Button type="submit" size="sm" busy={saving}>{t('common.save')}</Button>
          <PageNotice visible={!!form} role="status" tone={form?.ok ? 'success' : 'warning'}>
            {form?.ok ? t('voice.saved') : form?.error}
          </PageNotice>
        </form>
      </Card>
    {:else if allowance.reason === 'no_plan' || !allowance.reason}
      <Card>
        <div class="voicePanel">
          <Text class="noticeCopy"><bb-fit mode="block"><span data-fit="">{t('voice.free')}</span></bb-fit></Text>
          <TextLink href="/billing?from=voice" variant="inline"><bb-fit><span data-fit="">{t('voice.viewPremium')}</span></bb-fit></TextLink>
        </div>
      </Card>
    {/if}
  {:else}
    <AlertBanner tone="warning">
      <span class="noticeCopy"><bb-fit mode="block">{t('voice.unavailable')}</bb-fit></span>
    </AlertBanner>
  {/if}
</div>

<style>
  .voicePage {
    display: grid;
    gap: var(--bb-space-4);
    grid-template-columns: minmax(0, 1fr);
    max-inline-size: 48rem;
  }
  .voicePanel, .voiceField { display: grid; gap: var(--bb-space-3); min-inline-size: 0; }
  .usageHead, .usageDetails { display: grid; align-items: center; gap: var(--bb-space-2); }
  .usageHead { grid-template-columns: minmax(0, 1fr) minmax(5.75rem, 27%); }
  .usageDetails { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .noticeCopy { display: block; block-size: 2.75em; }
</style>
