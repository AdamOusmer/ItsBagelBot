<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { beforeNavigate, invalidateAll } from '$app/navigation';
  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { defaultPoweredConfig, poweredPreviewUrl, type PoweredConfig } from '@bagel/kit/overlays';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import { Autosave } from '#lib/autosave.svelte';
  import ReplaceLink from '../ReplaceLink.svelte';
  import { poweredFields } from './powered-form';
  import PoweredControls from './PoweredControls.svelte';
  import PoweredPreview from './PoweredPreview.svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();
  const { t } = getI18n();

  const loadedConfig = (): PoweredConfig => structuredClone(data.powered?.config ?? defaultPoweredConfig());

  let config = $state(untrack(loadedConfig));
  let url = $state(page.state?.poweredLink ?? '');
  let error = $state('');

  const autosave = new Autosave({
    revision: untrack(() => data.powered?.revision ?? 0),
    onConflict: async () => {
      await invalidateAll();
      syncPowered();
    },
    text: { failed: () => t('overlays.saveFailed'), conflict: () => t('overlays.conflict') }
  });

  const previewSrc = $derived(poweredPreviewUrl(config, data.isPremium === true, data.previewOrigin));
  const SAVE_PRIORITY: SaveState[] = ['error', 'saving', 'saved'];
  const saveState = $derived(SAVE_PRIORITY.find((state) => Object.values(autosave.status).includes(state)) ?? 'idle');

  function syncPowered() {
    config = loadedConfig();
    autosave.reset(data.powered?.revision ?? 0);
  }

  $effect(() => {
    void data.powered;
    untrack(syncPowered);
  });

  function change(key: keyof PoweredConfig, next: PoweredConfig) {
    config = next;
    autosave.schedule(key, { ...poweredFields(next), id: String(data.powered?.id ?? 0) });
  }

  function linked(next: string) {
    error = '';
    url = next;
  }

  beforeNavigate(() => void autosave.flush());
</script>

<svelte:head><title>{t('overlays.poweredTitle')}</title></svelte:head>

<section class="screen active studio" aria-labelledby="powered-title">
  <PageHead class="studio-head">
    <span class="page-title">
      <bb-fit class="page-title-copy"><span data-fit="" id="powered-title">{t('overlays.poweredTitle')}</span></bb-fit>
    </span>
    {#snippet trailing()}
      <div class="trail">
        <SaveStatus state={saveState} />
        <ButtonLink href="/overlays" variant="ghost" size="sm">
          <bb-fit><span data-fit="">{t('overlays.customBack')}</span></bb-fit>
        </ButtonLink>
      </div>
    {/snippet}
  </PageHead>
  <div class="intro bb-fit-box">
    <Text as="p" size="sm" tone="muted"><bb-fit mode="block">{t('overlays.poweredIntro')}</bb-fit></Text>
  </div>
  {#if data.powered}
    <div class="studio-grid">
      <div class="studio-main">
        <PoweredPreview src={previewSrc} />
      </div>
      <div class="studio-side">
        <PoweredControls {config} onChange={change} />
        <div class="link">
          <div class="layer" class:visible={!url} aria-hidden={!!url}>
            <Text as="div" size="xs" tone={error ? 'danger' : 'muted'}><bb-fit mode="block">{error || t('overlays.linkHidden')}</bb-fit></Text>
          </div>
          <div class="layer" class:visible={!!url} inert={!url}>
            {#if url}
              <CopySurface variant="well" text={url} aria-label={t('overlays.link')} legacyFallback
                onCopy={(copied) => (error = copied ? '' : t('overlays.copyFailed'))} />
            {/if}
          </div>
          <ReplaceLink id={data.powered.id} onLink={linked} onError={(message) => (error = message)} />
        </div>
      </div>
    </div>
  {:else}
    <PageNotice tone="warning">{t('overlays.unavailable')}</PageNotice>
  {/if}
</section>

<style>
  .page-title { display: flex; align-items: center; gap: var(--bb-space-3); white-space: nowrap; }
  .page-title-copy { min-inline-size: 0; }
  .studio :global(.studio-head) { margin-block-end: var(--bb-space-1); }
  .trail { display: flex; align-items: center; gap: var(--bb-space-3); }
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
  .studio-side { display: grid; gap: var(--bb-space-4); align-content: start; }
  .link {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-3);
    min-inline-size: 0;
  }
  .layer { grid-area: 1 / 1; display: grid; align-items: center; min-inline-size: 0; opacity: 0; }
  .layer.visible { opacity: 1; }
  @media (prefers-reduced-motion: no-preference) {
    .layer { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
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
  }
  @media (max-width: 640px) {
    .intro { block-size: 3em; line-height: 1.5; white-space: normal; align-content: start; }
  }
</style>
