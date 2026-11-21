<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { type SubmitFunction } from '$app/forms';
  import { beforeNavigate, invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { defaultPoweredConfig, poweredPreviewUrl, type PoweredConfig } from '@bagel/kit/overlays';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
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
    <PoweredPreview src={previewSrc} describedBy="powered-intro">
      {#snippet head()}
        <Heading level={6} as="h1" id="powered-title" class="studio-title" tabindex={-1}>
          <bb-fit><span data-fit="">{t('overlays.poweredTitle')}</span></bb-fit>
        </Heading>
        <span class="intro bb-fit-box" id="powered-intro">
          <Text as="span" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.poweredIntro')}</bb-fit></Text>
        </span>
      {/snippet}
      {#snippet status()}
        <SaveStatus state={saveState} />
      {/snippet}
    </PoweredPreview>
    <div class="docks">
      <PoweredLink {url} error={rotateError} busy={rotating} submit={submitRotate} />
      <PoweredControls {config} onChange={change} />
    </div>
  </section>
{/if}

<style>
  .page-title { display: flex; align-items: center; gap: var(--bb-space-3); white-space: nowrap; }
  .page-title-copy { min-inline-size: 0; }
  .studio {
    --studio-gap: var(--bb-space-3);
    --docks-h: 138px;
    display: flex;
    flex-direction: column;
    gap: var(--studio-gap);
  }
  .studio > :global(.scene) {
    --scene-head-h: 22px;
    --scene-gap: var(--bb-space-2);
    --card-pad: var(--bb-space-2) var(--bb-space-3) var(--bb-space-3);
    --scene-chrome: calc(var(--bb-space-2) + var(--scene-head-h) + var(--scene-gap) + var(--bb-space-3) + 2px);
  }
  .studio :global(.studio-title) { flex: none; margin: 0; }
  .intro {
    flex: 1;
    min-inline-size: 0;
    block-size: var(--scene-head-h);
    align-content: center;
    white-space: nowrap;
  }
  .docks {
    display: grid;
    grid-template-columns: minmax(0, 1.15fr) minmax(0, 0.8fr) minmax(0, 1.2fr);
    gap: var(--studio-gap);
  }
  .docks > :global(.dock) {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    gap: var(--bb-space-1);
    --card-pad: var(--bb-space-3) var(--bb-space-4);
  }
  @media (min-width: 1024px) {
    .studio :global(.studio-title) { max-inline-size: 40%; }
    .docks { block-size: var(--docks-h); }
    .studio > :global(.scene) {
      --stage-max-inline: max(
        20rem,
        calc((100dvh - var(--bb-topbar-height, 54px) - 10px - var(--gutter, 28px) - 6px - var(--bb-dock-clearance, 52px)
          - var(--scene-chrome) - var(--studio-gap) - var(--docks-h)) * 16 / 9)
      );
    }
  }
  @media (max-width: 1023px) {
    .intro { flex-basis: 100%; block-size: calc(2 * 1.5em); line-height: 1.5; white-space: normal; align-content: start; }
    .docks { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .docks > :global(.dock-wide) { grid-column: 1 / -1; }
  }
  @media (max-width: 640px) {
    .docks { grid-template-columns: minmax(0, 1fr); }
  }
</style>
