<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { type SubmitFunction } from '$app/forms';
  import { beforeNavigate, invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { defaultPoweredConfig, poweredPreviewUrl, type PoweredConfig } from '@bagel/kit/overlays';
  import EditorStage from '@bagel/ui/svelte/EditorStage.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import { Autosave } from '#lib/autosave.svelte';
  import { poweredFields } from './powered-form';
  import PoweredControls from './PoweredControls.svelte';
  import PoweredLink from './PoweredLink.svelte';
  import PoweredPreview from './PoweredPreview.svelte';
  import type { ActionData, PageData } from './$types';

  let { data, form }: { data: PageData; form: ActionData } = $props();
  const { t } = getI18n();

  const loadedConfig = (): PoweredConfig => structuredClone(data.powered?.config ?? defaultPoweredConfig());

  let config = $state(untrack(loadedConfig));
  let url = $state(untrack(() => data.powered?.url ?? ''));
  let rotating = $state(false);
  let rotateError = $state(untrack(() => form?.rotateError));

  const autosave = new Autosave({
    revision: untrack(() => data.powered?.revision ?? 0),
    onConflict: async () => {
      await invalidateAll();
      config = loadedConfig();
      autosave.reset(data.powered?.revision ?? 0);
    },
    text: { failed: () => t('overlays.saveFailed'), conflict: () => t('overlays.conflict') }
  });

  const previewSrc = $derived(poweredPreviewUrl(config, data.isPremium === true));
  const SAVE_PRIORITY: SaveState[] = ['error', 'saving', 'saved'];
  const saveState = $derived(SAVE_PRIORITY.find((state) => Object.values(autosave.status).includes(state)) ?? 'idle');

  function change(key: keyof PoweredConfig, next: PoweredConfig) {
    config = next;
    autosave.schedule(key, poweredFields(next));
  }

  beforeNavigate(() => void autosave.flush());

  let boardEl = $state<HTMLDivElement>();
  let boardLeft = $state(0);
  function measureBoard() {
    boardLeft = boardEl?.getBoundingClientRect().left ?? 0;
  }
  $effect(measureBoard);

  const submitRotate: SubmitFunction = async ({ cancel }) => {
    if (rotating) return cancel();
    rotating = true;
    rotateError = undefined;
    await autosave.flush();
    return async ({ update, result }) => {
      try {
        if (result.type === 'success' && typeof result.data?.url === 'string') {
          url = result.data.url;
          if (typeof result.data.revision === 'number') autosave.revision = result.data.revision;
        }
        if (result.type === 'failure' && typeof result.data?.rotateError === 'string') rotateError = result.data.rotateError;
        await update({ reset: false, invalidateAll: false });
      } finally {
        rotating = false;
      }
    };
  };
</script>

<svelte:head><title>{t('nav.overlays')}</title></svelte:head>
<svelte:window onresize={measureBoard} />

{#snippet topStart()}
  <span class="crumbs">
    <span class="crumb bb-fit-box"><Text as="span" size="sm" tone="muted"><bb-fit>{t('nav.overlays')}</bb-fit></Text></span>
    <span class="crumb-sep" aria-hidden="true">/</span>
    <h1 class="title bb-fit-box"><bb-fit>{t('overlays.poweredTitle')}</bb-fit></h1>
  </span>
{/snippet}

{#snippet topEnd()}
  <span class="save-slot">
    <span class="save-note bb-fit-box" class:quiet={saveState !== 'idle'}>
      <Text as="div" size="xs" tone="muted"><bb-fit mode="block">{t('modules.autoSaveNote')}</bb-fit></Text>
    </span>
    <span class="save-state"><SaveStatus state={saveState} /></span>
  </span>
{/snippet}

{#snippet rightPanel()}
  <aside class="panel" aria-labelledby="powered-title">
    <header class="head">
      <h2 class="name bb-fit-box" id="powered-title"><bb-fit>{t('overlays.poweredTitle')}</bb-fit></h2>
      <div class="intro bb-fit-box">
        <Text as="div" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.poweredIntro')}</bb-fit></Text>
      </div>
    </header>
    <div class="scroll">
      <PoweredLink {url} error={rotateError} busy={rotating} submit={submitRotate} />
      <PoweredControls {config} onChange={change} />
    </div>
  </aside>
{/snippet}

{#if data.readOnly || !data.powered}
  <section class="screen active">
    <PageHead>
      <span class="page-title">
        <bb-fit class="page-title-copy"><span data-fit="">{t('nav.overlays')}</span></bb-fit>
      </span>
    </PageHead>
    {#if data.readOnly}
      <PageNotice tone="info" role="note">{t('overlays.readOnly')}</PageNotice>
    {:else}
      <PageNotice tone="warning">{t('overlays.unavailable')}</PageNotice>
    {/if}
  </section>
{:else}
  <EditorStage
    class="overlays-stage"
    style="--stage-board-left: {boardLeft}px"
    bind:boardEl
    label={t('overlays.previewFrame')}
    height="100dvh"
    topInset="calc(var(--bb-topbar-height) + var(--overlays-top-gap, 6px))"
    bottomRoom="var(--overlays-bottom-room, 0px)"
    {topStart}
    {topEnd}
    {rightPanel}
  >
    <PoweredPreview src={previewSrc} />
  </EditorStage>
{/if}

<style>
  .page-title { display: flex; align-items: center; gap: var(--bb-space-3); white-space: nowrap; }
  .page-title-copy { min-inline-size: 0; }
  :global(.overlays-stage) {
    --overlays-top-gap: 6px;
    --overlays-bottom-room: 0px;
    --overlays-sheet-h: 50dvh;
    --stage-kb: var(--overlays-bottom-room);
    --stage-sheet-h: var(--overlays-sheet-h);
  }
  @media (max-width: 760px) {
    :global(.overlays-stage) { --overlays-top-gap: 10px; --overlays-sheet-h: 46dvh; }
  }
  @media (max-width: 760px), (pointer: coarse) and (max-height: 520px) {
    :global(.overlays-stage) { --overlays-bottom-room: calc(81px + 8px + env(safe-area-inset-bottom, 0px)); }
  }
  @media (max-width: 760px), (pointer: coarse) and (max-width: 1023px) {
    :global(.overlays-stage) {
      --overlays-scene-bottom: calc(var(--overlays-bottom-room) + min(var(--overlays-sheet-h), 100dvh - 64px) + 16px);
    }
  }
  .crumbs {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    min-inline-size: 0;
    padding-inline-start: var(--bb-space-3);
  }
  .crumb { flex: none; max-inline-size: 8rem; white-space: nowrap; }
  .crumb-sep { flex: none; color: var(--bb-muted); }
  .title {
    flex: 1;
    min-inline-size: 0;
    margin: 0;
    font: 600 var(--bb-text-md)/1.25 var(--bb-font-body);
    color: var(--bb-white);
    white-space: nowrap;
  }
  .save-slot {
    display: grid;
    align-items: center;
    inline-size: 11rem;
    padding-inline: var(--bb-space-2);
  }
  .save-note, .save-state {
    grid-area: 1 / 1;
    align-self: center;
    min-inline-size: 0;
  }
  .save-note { block-size: 2.5em; align-content: center; text-align: end; }
  .save-state { justify-self: end; white-space: nowrap; }
  .save-note.quiet { opacity: 0; }
  .panel {
    display: flex;
    flex-direction: column;
    block-size: 100%;
    min-block-size: 0;
    overflow: hidden;
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-1);
    box-shadow: var(--bb-depth-1), 0 16px 48px rgba(var(--bb-shadow-rgb), 0.55);
    --panel-pad: var(--bb-space-5);
  }
  .head {
    display: grid;
    flex: none;
    gap: var(--bb-space-1);
    padding: var(--bb-space-4) var(--panel-pad);
    border-block-end: 1px solid var(--bb-rule);
  }
  .name {
    block-size: var(--bb-space-5);
    margin: 0;
    font: 600 var(--bb-text-md)/1.25 var(--bb-font-body);
    color: var(--bb-white);
  }
  .intro { block-size: 2.5em; min-inline-size: 0; align-content: center; }
  .scroll {
    flex: 1;
    min-block-size: 0;
    overflow-y: auto;
    scrollbar-width: thin;
    overscroll-behavior: contain;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-6);
    padding: var(--bb-space-5) var(--panel-pad);
  }
  .scroll > :global(*) { flex: none; }
  @media (max-width: 760px) {
    .save-slot { inline-size: 8rem; }
    .crumb, .crumb-sep { display: none; }
  }
  @media (prefers-reduced-motion: no-preference) {
    .save-note { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
