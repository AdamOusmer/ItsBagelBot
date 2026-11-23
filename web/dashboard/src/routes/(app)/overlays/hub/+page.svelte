<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import CreatePortal from './CreatePortal.svelte';
  import OverlayList from './OverlayList.svelte';
  import PoweredBanner from './PoweredBanner.svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();
  const { t } = getI18n();

  type PortalStep = 'start' | 'import';

  let portalOpen = $state(false);
  let portalStep = $state<PortalStep>('start');
  let listBusy = $state(false);

  const canCreate = $derived(!data.readOnly && !!data.overlays);
  const powered = $derived(data.overlays?.find((overlay) => overlay.template === 'powered'));
  const custom = $derived(data.overlays?.filter((overlay) => overlay.kind === 'custom') ?? []);

  function openPortal(step: PortalStep) {
    portalStep = step;
    portalOpen = true;
  }
</script>

<svelte:head><title>{t('nav.overlays')}</title></svelte:head>

<section class="screen active overlays">
  <PageHead class="main-head">
    <span class="page-title">
      <bb-fit class="page-title-copy"><span data-fit="">{t('nav.overlays')}</span></bb-fit>
      <Chip as="span" pressed>{t('modules.betaChip')}</Chip>
    </span>
    {#snippet trailing()}
      <div class="head-actions" class:hidden={!canCreate} inert={!canCreate}>
        <Button class="head-action" variant="ghost" type="button" disabled={listBusy} onclick={() => openPortal('import')}>
          <Icon name="importFile" size={16} />
          <bb-fit class="action-label"><span data-fit="">{t('overlays.importOverlay')}</span></bb-fit>
        </Button>
        <Button class="head-action" type="button" disabled={listBusy} onclick={() => openPortal('start')}>
          <Icon name="plus" size={16} />
          <bb-fit class="action-label"><span data-fit="">{t('overlays.createOverlay')}</span></bb-fit>
        </Button>
      </div>
    {/snippet}
  </PageHead>
  <div class="intro bb-fit-box">
    <Text as="p" size="sm" tone="muted"><bb-fit mode="block">{t('overlays.subtitle')}</bb-fit></Text>
  </div>
  {#if data.readOnly}
    <PageNotice tone="info" role="note">{t('overlays.readOnly')}</PageNotice>
  {:else if !data.overlays}
    <PageNotice tone="warning">{t('overlays.unavailable')}</PageNotice>
  {:else}
    <PoweredBanner {powered} />
    <OverlayList overlays={custom} bind:busy={listBusy} onCreate={() => openPortal('start')} />
    <CreatePortal bind:open={portalOpen} bind:step={portalStep} />
  {/if}
</section>

<style>
  .page-title { display: flex; align-items: center; gap: var(--bb-space-3); white-space: nowrap; }
  .page-title-copy { min-inline-size: 0; }
  .overlays :global(.main-head) { margin-block-end: var(--bb-space-1); }
  .head-actions { display: flex; align-items: center; gap: var(--bb-space-3); }
  .head-actions.hidden { opacity: 0; visibility: hidden; }
  .head-actions :global(.head-action) { inline-size: 10rem; }
  .action-label { line-height: 1.4; }
  .intro {
    block-size: 1.5rem;
    min-inline-size: 0;
    margin-block-end: var(--bb-space-5);
    align-content: center;
    white-space: nowrap;
  }
  @media (max-width: 640px) {
    .head-actions :global(.head-action) { inline-size: 8.5rem; }
    .intro { block-size: 3em; line-height: 1.5; white-space: normal; align-content: start; }
  }
</style>
