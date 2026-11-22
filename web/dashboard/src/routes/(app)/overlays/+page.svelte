<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { type SubmitFunction } from '$app/forms';
  import { beforeNavigate, invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { defaultPoweredConfig, poweredPreviewUrl, type PoweredConfig } from '@bagel/kit/overlays';
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

  const previewSrc = $derived(poweredPreviewUrl(config, data.isPremium === true, data.previewOrigin));
  const SAVE_PRIORITY: SaveState[] = ['error', 'saving', 'saved'];
  const saveState = $derived(SAVE_PRIORITY.find((state) => Object.values(autosave.status).includes(state)) ?? 'idle');

  function change(key: keyof PoweredConfig, next: PoweredConfig) {
    config = next;
    autosave.schedule(key, poweredFields(next));
  }

  beforeNavigate(() => void autosave.flush());

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
  <section class="screen active studio" aria-labelledby="powered-title">
    <PageHead compact class="studio-head">
      <span class="page-title">
        <bb-fit class="page-title-copy"><span data-fit="" id="powered-title">{t('overlays.poweredTitle')}</span></bb-fit>
      </span>
      {#snippet trailing()}
        <SaveStatus state={saveState} />
      {/snippet}
    </PageHead>
    <div class="intro bb-fit-box">
      <Text as="p" size="sm" tone="muted"><bb-fit mode="block">{t('overlays.poweredIntro')}</bb-fit></Text>
    </div>
    <div class="studio-grid">
      <div class="studio-main">
        <PoweredPreview src={previewSrc} />
        <PoweredLink {url} error={rotateError} busy={rotating} submit={submitRotate} />
      </div>
      <div class="studio-side">
        <PoweredControls {config} onChange={change} />
      </div>
    </div>
  </section>
{/if}

<style>
  .page-title { display: flex; align-items: center; gap: var(--bb-space-3); white-space: nowrap; }
  .page-title-copy { min-inline-size: 0; }
  .studio :global(.studio-head) { margin-block-end: var(--bb-space-2); }
  .intro {
    block-size: 1.5rem;
    min-inline-size: 0;
    margin-block-end: var(--bb-space-5);
    align-content: center;
    white-space: nowrap;
  }
  .studio-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-4);
  }
  .studio-main {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-4);
    min-inline-size: 0;
  }
  .studio-side {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-4);
  }
  @media (min-width: 1024px) {
    .studio {
      display: flex;
      flex-direction: column;
      block-size: calc(100dvh - var(--bb-topbar-height) - 10px - var(--gutter, 28px) - 6px - var(--bb-dock-clearance));
    }
    .studio-grid {
      flex: 1;
      min-block-size: 0;
      grid-template-columns: minmax(0, 2fr) minmax(20rem, 1fr);
    }
    .studio-main { min-block-size: 0; }
    .studio-main > :global(.scene) { flex: 1; min-block-size: 0; }
    .studio-side { display: flex; flex-direction: column; }
    .studio-side > :global(.dock) { display: flex; flex: 1; flex-direction: column; }
  }
  @media (max-width: 640px) {
    .intro { block-size: 3em; line-height: 1.5; white-space: normal; align-content: start; }
    .studio-side { grid-template-columns: minmax(0, 1fr); }
  }
</style>
