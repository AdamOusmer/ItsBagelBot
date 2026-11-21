<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { enhance, type ActionResult, type SubmitFunction } from '$app/forms';
  import { beforeNavigate, goto, invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import {
    CLOUD_VOICE_CATALOG, DEFAULT_VOICE, MAX_CLIP_SECONDS, MIN_CLIP_SECONDS,
    VOICE_ALERT_KINDS, VOICE_CHARS_PER_SECOND, VOICE_GROUPS
  } from '@bagel/kit/voice-plan';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import PremiumGate from '#lib/components/shared/PremiumGate.svelte';
  import VoiceAlertCard from './VoiceAlertCard.svelte';
  import VoiceOverlayCard from './VoiceOverlayCard.svelte';
  import VoiceUsageCard from './VoiceUsageCard.svelte';
  import type { ActionData, PageData } from './$types';

  let { data, form }: { data: PageData; form: ActionData } = $props();
  const { t, locale } = getI18n();
  const numbers = new Intl.NumberFormat(locale);
  const groups = ['energetic', 'warm', 'calm', 'authoritative'] as const;
  const options = groups.flatMap((group) => CLOUD_VOICE_CATALOG
    .filter(([, character]) => VOICE_GROUPS[character] === group)
    .map(([name, character]) => ({
      value: name,
      label: name,
      description: t(`voice.character.${character.toLowerCase()}`),
      group: t(`voice.group.${group}`),
      searchText: character
    })));

  type Draft = {
    enabled: boolean;
    voice: string;
    maxSeconds: number | string | null | undefined;
    config: Record<string, string>;
    revision: number;
  };
  const allowance = $derived(data.readOnly ? null : data.allowance);
  const loaded: Draft = $derived({
    enabled: allowance?.enabled ?? false,
    voice: allowance?.voice || DEFAULT_VOICE,
    maxSeconds: allowance?.max_seconds ?? MAX_CLIP_SECONDS,
    config: data.alerts?.config ?? {},
    revision: data.alerts?.revision ?? 0
  });
  const fingerprint = (settings: Draft) => JSON.stringify(settings);

  let draft = $state<Draft>(untrack(() => ({ ...loaded, config: { ...loaded.config } })));
  let baseline = $state(untrack(() => fingerprint(loaded)));
  let resetKey = $state(0);
  let pending = $state<'save' | 'rotateOverlay' | 'refresh' | null>(null);
  let overlayUrl = $state(untrack(() => form?.overlayUrl));
  let overlayError = $state(untrack(() => form?.overlayError));
  let conflict = $state(untrack(() => form?.conflict === true));
  let saveError = $state(untrack(() => form?.error));
  let saved = $state(untrack(() => form?.ok === true));
  const dirty = $derived(fingerprint(draft) !== baseline);
  const saving = $derived(pending === 'save');
  const status = $derived.by(() => {
    if (saving) return 'saving';
    if (conflict) return 'conflict';
    if (saveError) return 'error';
    return saved && !dirty ? 'saved' : 'idle';
  });
  const errorLabel = $derived(conflict ? t('voice.conflict') : saveError);
  const overrides = $derived([{ value: '', label: t('voice.useDefault', { voice: draft.voice }) }, ...options]);
  const minBits = $derived(Number(draft.config.ttsMinBits ?? 0));
  const approxChars = $derived(numbers.format((Number(draft.maxSeconds) || 0) * VOICE_CHARS_PER_SECOND));

  function adopt(settings: Draft) {
    draft = { ...settings, config: { ...settings.config } };
    baseline = fingerprint(settings);
    resetKey += 1;
  }

  $effect(() => {
    const settings = loaded;
    untrack(() => {
      if (dirty || pending) return;
      if (fingerprint(settings) === baseline) return;
      adopt(settings);
    });
  });

  const discard = createDiscardGuard(() => dirty);

  async function resetDraft() {
    if (pending) return;
    pending = 'refresh';
    try {
      if (conflict) await invalidateAll();
      adopt(loaded);
      conflict = false;
      saveError = undefined;
      saved = false;
    } finally {
      pending = null;
    }
  }

  beforeNavigate(({ cancel, to, willUnload }) => {
    if (!dirty) return;
    cancel();
    if (willUnload || !to) return;
    discard.guard(() => {
      adopt(loaded);
      void goto(to.url);
    });
  });

  function applySaveResult(result: ActionResult) {
    const failure = result.type === 'failure' ? result.data : undefined;
    saved = result.type === 'success' && result.data?.ok === true;
    conflict = failure?.conflict === true;
    saveError = typeof failure?.error === 'string' ? failure.error : undefined;
  }

  const submitSave: SubmitFunction = ({ cancel }) => {
    if (pending || conflict) return cancel();
    pending = 'save';
    return async ({ update, result }) => {
      try {
        applySaveResult(result);
        await update({ reset: false, invalidateAll: saved });
        if (saved) adopt(loaded);
      } finally {
        pending = null;
      }
    };
  };

  const submitOverlay: SubmitFunction = ({ cancel }) => {
    if (pending) return cancel();
    pending = 'rotateOverlay';
    overlayError = undefined;
    return async ({ update, result }) => {
      try {
        if (result.type === 'success' && typeof result.data?.overlayUrl === 'string') overlayUrl = result.data.overlayUrl;
        if (result.type === 'failure' && typeof result.data?.overlayError === 'string') overlayError = result.data.overlayError;
        await update({ reset: false, invalidateAll: false });
      } finally {
        pending = null;
      }
    };
  };
</script>

<svelte:head><title>{t('nav.voice')}</title></svelte:head>

{#if data.locked}
  <PremiumGate title={t('voice.gateTitle')} body={t('voice.gateBody')} />
{:else}
  <section class="screen active">
    <PageHead>
      <span class="page-title">
        <bb-fit><span data-fit="">{t('nav.voice')}</span></bb-fit>
        <Chip as="span" pressed>{t('modules.betaChip')}</Chip>
      </span>
    </PageHead>

    {#if data.readOnly}
      <PageNotice tone="info" role="note">{t('voice.readOnly')}</PageNotice>
    {:else if !allowance}
      <PageNotice tone="warning">{t('voice.unavailable')}</PageNotice>
    {:else if allowance.reason === 'inactive' || allowance.reason === 'banned'}
      {@const inactive = allowance.reason === 'inactive'}
      <Card as="section" aria-labelledby="voice-status">
        <div class="status-head">
          <Heading level={6} as="h2" id="voice-status">
            <bb-fit><span data-fit="">{t('voice.settingsTitle')}</span></bb-fit>
          </Heading>
          <div class="status-tag">
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
      <div class="paths">
        <VoiceUsageCard {allowance} />
        {#if allowance.limit_tokens > 0}
          <VoiceOverlayCard configured={!!overlayUrl || allowance.overlay_configured === true} url={overlayUrl}
            error={overlayError} busy={pending === 'rotateOverlay'} disabled={pending !== null} submit={submitOverlay} />
        {/if}
      </div>

      {#if data.alerts}
        <form class="settings" method="POST" action="?/save" use:enhance={submitSave} aria-busy={saving}>
          <div class="settings" inert={saving || pending === 'refresh'}>
            <input type="hidden" name="expected_rev" value={draft.revision} />
            <Card as="section" aria-labelledby="voice-settings">
              <div class="global">
                <Heading level={6} as="h2" id="voice-settings">
                  <bb-fit><span data-fit="">{t('voice.settingsTitle')}</span></bb-fit>
                </Heading>
                <SwitchRow control="end" fitCopy bind:checked={draft.enabled}
                  label={t('voice.enable')} hint={t('voice.fallback')} hintId="voice-fallback" />
                <input type="hidden" name="enabled" value={String(draft.enabled)} />
                <Field label={t('voice.choose')} fitCopy>
                  <Select name="voice" label={t('voice.choose')} bind:value={draft.voice} {options} searchable required fill />
                </Field>
                <SwitchRow control="end" fitCopy checked={draft.config.ttsLiveOnly === 'true'} label={t('voice.liveOnly')}
                  onCheckedChange={(checked) => (draft.config.ttsLiveOnly = String(checked))} />
                <input type="hidden" name="ttsLiveOnly" value={draft.config.ttsLiveOnly} />
                <SwitchRow control="end" fitCopy checked={draft.config.ttsReadMessages === 'true'} label={t('voice.readMessages')}
                  hint={t('voice.readMessagesHint')} hintId="voice-read-messages"
                  onCheckedChange={(checked) => (draft.config.ttsReadMessages = String(checked))} />
                <input type="hidden" name="ttsReadMessages" value={draft.config.ttsReadMessages} />
                <div class="limits">
                  <Field label={t('voice.minBits')} fitCopy>
                    <Input name="ttsMinBits" type="number" min={0} max={Number.MAX_SAFE_INTEGER} step={1}
                      value={draft.config.ttsMinBits}
                      oninput={(event) => (draft.config.ttsMinBits = event.currentTarget.value)} required fill />
                  </Field>
                  <Field label={t('voice.maxLength')} fitCopy>
                    <Input name="max_seconds" type="number" min={MIN_CLIP_SECONDS} max={MAX_CLIP_SECONDS} step={1}
                      bind:value={draft.maxSeconds} aria-describedby="voice-approx-length" required fill />
                  </Field>
                  <div class="length-hint" id="voice-approx-length">
                    <Text as="div" size="sm" tone="muted">
                      <bb-fit><span data-fit="">{t('voice.maxLengthHint', { chars: approxChars })}</span></bb-fit>
                    </Text>
                  </div>
                </div>
              </div>
            </Card>
            <section class="alert-section" aria-labelledby="voice-alerts">
              <Heading level={6} as="h2" id="voice-alerts">
                <bb-fit><span data-fit="">{t('voice.alertsTitle')}</span></bb-fit>
              </Heading>
              <div class="alert-grid">
                {#each VOICE_ALERT_KINDS as kind (kind)}
                  <VoiceAlertCard {kind} bind:config={draft.config} options={overrides} {minBits} {resetKey} />
                {/each}
              </div>
            </section>
          </div>
          <EditorFooter class="voice-footer" {dirty} {status} canSave={dirty && !pending && !conflict}
            {errorLabel} title={errorLabel} savedLabel={t('voice.saved')} onCancel={() => discard.guard(() => void resetDraft())} />
        </form>
      {:else}
        <div class="settings"><PageNotice tone="warning">{t('voice.unavailable')}</PageNotice></div>
      {/if}
    {/if}
  </section>

  <ConfirmDialog open={discard.open} title={t('modules.discardTitle')} body={t('modules.discardBody')}
    confirmLabel={t('modules.discard')} cancelLabel={t('modules.keepEditing')}
    onCancel={discard.cancel} onConfirm={discard.confirm} tone="danger" />
{/if}

<style>
  .page-title { display: inline-flex; align-items: center; gap: var(--bb-space-3); max-inline-size: 100%; }
  .paths, .settings, .alert-grid, .global, .alert-section, .limits, .status-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-4);
  }
  .paths { align-items: start; }
  .settings { margin-block-start: var(--bb-space-4); }
  .settings > .settings { margin-block-start: 0; }
  .global, .alert-section, .limits, .status-head { gap: var(--bb-space-3); }
  .alert-grid { grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr)); }
  .length-hint { block-size: 1.25rem; margin-block-start: calc(-1 * var(--bb-space-1)); }
  .status-head { grid-template-columns: minmax(0, 1fr) minmax(0, 48%); align-items: center; }
  .status-tag { justify-self: end; min-inline-size: 0; max-inline-size: 100%; }
  .status-copy { block-size: 2.75rem; min-inline-size: 0; align-content: center; }
  :global(.voice-footer) { z-index: 2; }

  @media (min-width: 600px) {
    .limits { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .length-hint { grid-column: 2; }
  }

  @media (min-width: 1080px) {
    .paths { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
</style>
