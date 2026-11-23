<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { type SubmitFunction } from '$app/forms';
  import { beforeNavigate, invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import {
    alertVoiceKeys, DEFAULT_VOICE, MAX_CLIP_SECONDS, VOICE_ALERT_KINDS, type VoiceAlertKind
  } from '@bagel/kit/voice-plan';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import PremiumGate from '#lib/components/shared/PremiumGate.svelte';
  import { voiceOptions } from '#lib/components/voice/voice-options';
  import VoicePreview from '#lib/components/voice/VoicePreview.svelte';
  import { Autosave } from '#lib/autosave.svelte';
  import VoiceBlocksCard from './VoiceBlocksCard.svelte';
  import { utcDateTime } from './voice-settings';
  import VoiceAlertEditor from './VoiceAlertEditor.svelte';
  import VoiceChannelPointsCard from './VoiceChannelPointsCard.svelte';
  import VoiceChatCommandCard from './VoiceChatCommandCard.svelte';
  import VoiceAlertRow from './VoiceAlertRow.svelte';
  import VoiceOverlayCard from './VoiceOverlayCard.svelte';
  import VoiceSettingsCard from './VoiceSettingsCard.svelte';
  import VoiceUsageCard from './VoiceUsageCard.svelte';
  import type { ActionData, PageData } from './$types';

  let { data, form }: { data: PageData; form: ActionData } = $props();
  const { t } = getI18n();
  const options = voiceOptions(t);

  const allowance = $derived(data.readOnly ? null : data.allowance);
  const overlay = $derived(data.readOnly ? null : data.overlay);
  const safety = $derived(data.readOnly ? null : data.safety);
  const pausedUntil = $derived(safety?.pausedUntil ?? 0);
  const paused = $derived(pausedUntil > Date.now());
  const pausedTimes = utcDateTime(getI18n().locale);
  const pausedCopy = $derived(paused ? t('voice.paused', { time: pausedTimes.format(new Date(pausedUntil)) }) : '');

  function loadedValues(): Record<string, string> {
    return {
      ...data.alerts?.config,
      enabled: String(overlay?.settings.enabled ?? false),
      voice: overlay?.settings.voice || DEFAULT_VOICE,
      max_seconds: String(overlay?.settings.max_seconds ?? MAX_CLIP_SECONDS)
    };
  }

  let values = $state(untrack(loadedValues));

  function loadedCommand(): Record<string, string> {
    const command = data.readOnly ? null : data.command;
    return { ...command?.config, enabled: String(command?.enabled ?? false) };
  }

  let commandValues = $state(untrack(loadedCommand));
  let selected = $state<VoiceAlertKind | null>(null);
  type VoiceAction = 'rotateOverlay' | 'testAlert';
  let pending = $state<VoiceAction | null>(null);
  let overlayUrl = $state(untrack(() => form?.overlayUrl));
  let overlayError = $state(untrack(() => form?.overlayError));

  const autosave = new Autosave({
    revision: untrack(() => data.alerts?.revision ?? 0),
    onConflict: async () => {
      await invalidateAll();
      values = loadedValues();
      autosave.reset(data.alerts?.revision ?? 0);
    },
    text: { failed: () => t('voice.saveFailed'), conflict: () => t('voice.conflict') }
  });

  const commandSave = new Autosave({
    action: 'saveCommand',
    revision: untrack(() => (data.readOnly ? null : data.command)?.revision ?? 0),
    onConflict: async () => {
      await invalidateAll();
      commandValues = loadedCommand();
      commandSave.reset((data.readOnly ? null : data.command)?.revision ?? 0);
    },
    text: { failed: () => t('voice.saveFailed'), conflict: () => t('voice.conflict') }
  });

  async function changeCommand(key: string, value: string) {
    const before = commandValues[key] ?? '';
    if (value === before) return;
    commandValues[key] = value;
    if (await commandSave.save(key, { [key]: value }) === 'failed') commandValues[key] = before;
  }

  const overrides = $derived([{ value: '', label: t('voice.useDefault', { voice: values.voice }) }, ...options]);

  async function change(key: string, value: string) {
    const before = values[key] ?? '';
    if (value === before) return;
    values[key] = value;
    if (await autosave.save(key, { [key]: value }) === 'failed') values[key] = before;
  }

  function alertStatus(kind: VoiceAlertKind): SaveState {
    const states = Object.values(alertVoiceKeys(kind)).map((key) => autosave.status[key] ?? 'idle');
    return (['saving', 'error', 'saved'] as const).find((state) => states.includes(state)) ?? 'idle';
  }

  function openAlert(kind: VoiceAlertKind) {
    void autosave.flush();
    selected = selected === kind ? null : kind;
  }

  function closeAlert() {
    void autosave.flush();
    selected = null;
  }

  beforeNavigate(() => {
    void autosave.flush();
    void commandSave.flush();
  });

  $effect(() => {
    if (form?.testAlertSent) toast('success', untrack(() => t('voice.testSent')));
    if (form?.testAlertError) toast('danger', form.testAlertError);
  });

  const submitVoice = (action: VoiceAction): SubmitFunction => ({ cancel }) => {
    if (pending) return cancel();
    pending = action;
    if (action === 'rotateOverlay') overlayError = undefined;
    return async ({ update, result }) => {
      try {
        if (result.type === 'success' && typeof result.data?.overlayUrl === 'string') overlayUrl = result.data.overlayUrl;
        if (result.type === 'failure' && typeof result.data?.overlayError === 'string') overlayError = result.data.overlayError;
        if (result.type === 'error' && action === 'testAlert') {
          toast('danger', t('voice.testFailed'));
          return;
        }
        await update({ reset: false, invalidateAll: false });
      } finally {
        pending = null;
      }
    };
  };
  const submitOverlay = submitVoice('rotateOverlay');
  const submitTestAlert = submitVoice('testAlert');
</script>

<svelte:head><title>{t('nav.voice')}</title></svelte:head>

{#if data.locked}
  <PremiumGate><VoicePreview /></PremiumGate>
{:else}
  <section class="screen active">
    <PageHead>
      <span class="page-title">
        <bb-fit class="page-title-copy"><span data-fit="">{t('nav.voice')}</span></bb-fit>
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
      <div class="stack">
        <PageNotice tone="warning" visible={paused}>{pausedCopy}</PageNotice>
        <VoiceUsageCard {allowance} settings={overlay?.settings} />
        {#if allowance.limit_tokens > 0 && overlay}
          <VoiceOverlayCard configured={!!overlayUrl || overlay.configured} url={overlayUrl}
            error={overlayError} busy={pending === 'rotateOverlay'} disabled={pending !== null} submit={submitOverlay}
            testBusy={pending === 'testAlert'} {submitTestAlert} />
        {/if}

        {#if data.alerts}
          <VoiceSettingsCard {values} status={autosave.status} {options} onChange={change} />

          <DeckLayout inspecting={selected !== null}>
            <DeckList>
              <div class="section-head">
                <Heading level={6} as="h2" variant="eyebrow" id="voice-alerts">
                  <bb-fit><span data-fit="">{t('voice.alertsTitle')}</span></bb-fit>
                </Heading>
                <Text as="p" size="xs" tone="muted">{t('voice.alertsHint')}</Text>
              </div>
              <Divider flush />
              <ul class="list" aria-labelledby="voice-alerts">
                {#each VOICE_ALERT_KINDS as kind, i (kind)}
                  {@const keys = alertVoiceKeys(kind)}
                  <li>
                    <VoiceAlertRow {kind} index={i + 1} minBits={Number(values.ttsMinBits ?? 0)}
                      enabled={values[keys.enabled] === 'true'} status={alertStatus(kind)} expanded={selected === kind}
                      onExpand={() => openAlert(kind)} onToggle={(checked) => change(keys.enabled, String(checked))} />
                  </li>
                {/each}
              </ul>
            </DeckList>

            {#if selected}
              <InspectorSurface open title={t(`voice.kind.${selected}`)} controls="voice-editor"
                closeLabel={t('modules.closeEditor')} onClose={closeAlert}>
                <Scroller fill padding="16px" smooth>
                  {#key selected}
                    <VoiceAlertEditor kind={selected} {values} options={overrides} onChange={change} />
                  {/key}
                </Scroller>
              </InspectorSurface>
            {/if}
          </DeckLayout>

          {#if data.command}
            <VoiceChatCommandCard values={commandValues} status={commandSave.status} onChange={changeCommand} />
          {/if}
          {#if safety}
            <VoiceBlocksCard manual={safety.manual} error={form?.blockError} />
          {/if}
          <VoiceChannelPointsCard />
        {:else}
          <PageNotice tone="warning">{t('voice.unavailable')}</PageNotice>
        {/if}
      </div>
    {/if}
  </section>
{/if}

<style>
  .page-title { display: flex; align-items: center; gap: var(--bb-space-3); white-space: nowrap; }
  .page-title-copy { min-inline-size: 0; }
  .stack, .status-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-4);
  }
  .status-head { grid-template-columns: minmax(0, 1fr) minmax(0, 48%); align-items: center; gap: var(--bb-space-3); }
  .status-tag { justify-self: end; min-inline-size: 0; max-inline-size: 100%; }
  .status-copy { block-size: 2.75rem; min-inline-size: 0; align-content: center; }
  .section-head { display: flex; flex-direction: column; gap: var(--bb-space-1); padding: 12px 18px; }
  .list { list-style: none; margin: 0; padding: 0; }
</style>
