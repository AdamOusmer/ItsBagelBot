<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { CLOUD_VOICES, DEFAULT_VOICE, MAX_CLIP_SECONDS, MIN_CLIP_SECONDS, VOICE_TOKENS_PER_SECOND } from '@bagel/kit/voice-plan';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import type { ActionData, PageData } from './$types';

  let { data, form }: { data: PageData; form: ActionData } = $props();
  const { t, locale } = getI18n();
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });
  const numbers = new Intl.NumberFormat(locale);
  const options = CLOUD_VOICES.map((name) => ({ value: name, label: name }));
  const plans = {
    free: { label: t('planLabel.free'), tone: 'free' },
    premium: { label: t('voice.planPremium'), tone: 'paid' },
    giveaway: { label: t('voice.planGiveaway'), tone: 'paid' },
    admin_grant: { label: t('voice.planAdmin'), tone: 'paid' },
    vip: { label: t('planLabel.vip'), tone: 'vip' }
  } as const;

  function duration(tokens: number): string {
    const total = Math.floor(tokens / VOICE_TOKENS_PER_SECOND);
    const minutes = t('common.duration.minutes', { n: numbers.format(Math.floor(total / 60)) });
    const seconds = t('common.duration.seconds', { n: numbers.format(total % 60) });
    return `${minutes} ${seconds}`;
  }

  const allowance = $derived(data.readOnly ? null : data.allowance);
  const showUsage = $derived(allowance?.reason === 'ok' || allowance?.reason === 'no_plan');
  let enabled = $derived(allowance?.enabled ?? false);
  let voice: string = $derived(allowance?.voice ?? DEFAULT_VOICE);
  let saving = $state(false);
  const saveMessage = $derived(form?.ok ? t('voice.saved') : form?.error);

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

<section class="screen active">
  <PageHead><bb-fit><span data-fit="">{t('nav.voice')}</span></bb-fit></PageHead>

  <div class="paths" class:split={showUsage}>
    {#if data.readOnly}
      <PageNotice tone="info" role="note">{t('voice.readOnly')}</PageNotice>
    {:else if !allowance}
      <PageNotice tone="warning">{t('voice.unavailable')}</PageNotice>
    {:else if allowance.reason === 'inactive' || allowance.reason === 'banned'}
      {@const inactive = allowance.reason === 'inactive'}
      <Card as="section" aria-labelledby="voice-status">
        <div class="card-head">
          <Heading level={6} as="h2" id="voice-status">
            <bb-fit><span data-fit="">{t('voice.settingsTitle')}</span></bb-fit>
          </Heading>
          <div class="plan">
            <Tag tone={allowance.reason} bare contain>
              <bb-fit><span data-fit="">{inactive ? t('voice.inactiveLabel') : t('voice.bannedLabel')}</span></bb-fit>
            </Tag>
          </div>
        </div>
        <div class="status-copy bb-fit-box">
          <Text as="div" size="sm" tone="muted">
            <bb-fit mode="block">{inactive ? t('voice.inactive') : t('voice.banned')}</bb-fit>
          </Text>
        </div>
      </Card>
    {:else}
      {@const plan = plans[allowance.plan]}
      {@const usage = t('voice.usage', {
        used: duration(allowance.used_tokens),
        limit: numbers.format(allowance.limit_tokens / (VOICE_TOKENS_PER_SECOND * 60))
      })}
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
          <Text as="div" size="lg"><bb-fit><span data-fit="">{usage}</span></bb-fit></Text>
          <ProgressBar size="sm" label={t('voice.usageTitle')} aria-valuetext={usage}
            value={allowance.limit_tokens ? allowance.used_tokens / allowance.limit_tokens : 0} />
          <Text as="div" size="sm" tone="muted"><bb-fit><span data-fit="">
            {t('voice.resets', { date: dates.format(new Date(allowance.resets_at)) })}
          </span></bb-fit></Text>
        </div>
      </Card>
    {/if}

    {#if allowance?.reason === 'ok'}
      <Card as="section" aria-labelledby="voice-settings">
        <div class="card-head">
          <Heading level={6} as="h2" id="voice-settings">
            <bb-fit><span data-fit="">{t('voice.settingsTitle')}</span></bb-fit>
          </Heading>
        </div>
        <form method="POST" action="?/save" use:enhance={submit}>
          <div class="enable-row">
            <SwitchRow control="end" fitCopy bind:checked={enabled} disabled={saving}
              label={t('voice.enable')} hint={t('voice.fallback')} hintId="voice-fallback" />
            <input type="hidden" name="enabled" value={String(enabled)} />
          </div>
          <Field label={t('voice.choose')} fitCopy>
            <Select name="voice" label={t('voice.choose')} bind:value={voice} {options} disabled={saving} required fill />
          </Field>
          <Field label={t('voice.maxLength')} fitCopy>
            <Input name="max_seconds" type="number" min={MIN_CLIP_SECONDS} max={MAX_CLIP_SECONDS} step={1}
              value={allowance.max_seconds ?? MAX_CLIP_SECONDS} disabled={saving} required fill />
          </Field>
          <div class="save-row">
            <Button variant="secondary" type="submit" size="sm" busy={saving}>
              <bb-fit><span data-fit="">{t('common.save')}</span></bb-fit>
            </Button>
            <div class="status-copy bb-fit-box" role="status">
              <Text as="div" size="sm" tone={form?.ok ? 'success' : 'danger'}>
                <bb-fit mode="block">{saving ? '' : saveMessage}</bb-fit>
              </Text>
            </div>
          </div>
        </form>
      </Card>
    {:else if allowance?.reason === 'no_plan'}
      <Card flush>
        <EmptyState class="local-voice" title={t('voice.settingsTitle')} body={t('voice.free')}>
          <ButtonLink href="/billing?from=voice" variant="secondary" size="sm">
            <bb-fit><span data-fit="">{t('voice.viewPremium')}</span></bb-fit>
          </ButtonLink>
        </EmptyState>
      </Card>
    {/if}
  </div>
</section>

<style>
  .paths {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: start;
    gap: var(--bb-space-4);
  }
  .card-head, .save-row { align-items: center; gap: var(--bb-space-3); }
  .card-head { display: grid; margin-bottom: var(--bb-space-3); }
  .card-head:has(.plan) { grid-template-columns: minmax(0, 1fr) minmax(0, 48%); }
  .plan { justify-self: end; min-inline-size: 0; max-inline-size: 100%; }
  .meter { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-2); }
  .enable-row { margin-bottom: var(--bb-space-3); }
  .status-copy { block-size: 2.75rem; min-inline-size: 0; align-content: center; }
  .save-row { display: flex; --btn-w: 7rem; --btn-justify: center; }
  .save-row .status-copy { flex: 1; }
  .paths :global(.local-voice) {
    display: grid;
    grid-template-rows: 1.5rem 3.75rem auto;
    --btn-w: 10rem;
    --btn-justify: center;
  }
  @media (min-width: 1080px) {
    .paths.split { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
</style>
