<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { beforeNavigate, goto } from '$app/navigation';
  import { untrack } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import OverlayEditor from '@bagel/kit/components/overlay-editor/OverlayEditor.svelte';
  import type { OverlayDocument } from '@bagel/kit/overlay-document';
  import {
    blankDocument, createOverlay, fromServed, templateNames, type SaveOutcome, type Template
  } from '@bagel/kit/overlay-editor';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import PremiumGate from '#lib/components/shared/PremiumGate.svelte';
  import { premiumLocked } from '#lib/components/shared/premium-lock';
  import { dashboardMedia } from '#lib/overlay-media';
  import ImportPanel from './ImportPanel.svelte';
  import { postSave } from './save-client';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();
  const { t } = getI18n();

  const PREVIEW_URL = '/overlays/custom?preview=1';

  let seed = $state.raw<OverlayDocument>(blankDocument(t('overlayEditor.page.newName')));
  let overlayId = $state(0);
  let dirty = $state(false);

  function adopt(overlay: PageData['overlay']): void {
    seed = overlay?.document ? fromServed(overlay.document) : blankDocument(t('overlayEditor.page.newName'));
    overlayId = overlay?.id ?? 0;
  }

  untrack(() => adopt(data.overlay));
  $effect(() => {
    const overlay = data.overlay;
    untrack(() => {
      if ((overlay?.id ?? 0) !== overlayId) adopt(overlay);
    });
  });

  const locked = $derived(premiumLocked(data));
  const media = $derived(locked ? undefined : dashboardMedia());
  const creating = $derived(overlayId === 0);
  const kindName = (kind: string): string => t(`overlayEditor.kind.${kind}`);

  beforeNavigate(({ cancel, type }) => {
    if (dirty && type !== 'goto' && !confirm(t('overlayEditor.page.discard'))) cancel();
  });

  async function save(doc: OverlayDocument): Promise<SaveOutcome> {
    const reply = await postSave(overlayId, doc);
    if (!reply.ok) return { ok: false, error: reply.error || t('overlayEditor.save.failed') };
    if (overlayId === 0) void goto(`/overlays/custom?id=${reply.id}`, { replaceState: true });
    overlayId = reply.id;
    return reply;
  }

  function start(template: Template): void {
    seed = createOverlay(template, t(`overlayEditor.template.${template}`), kindName);
  }

  const openImported = (id: number): void => void goto(`/overlays/custom?id=${id}`, { invalidateAll: true });
</script>

<svelte:head><title>{t('overlays.customTitle')}</title></svelte:head>

{#if locked && !data.preview}
  <PremiumGate trial={{ label: t('overlayEditor.page.try'), href: PREVIEW_URL }} />
{:else}
  <section class="screen active">
    <PageHead>
      <bb-fit class="page-title"><span data-fit="">{t('overlays.customTitle')}</span></bb-fit>
      {#snippet trailing()}
        <ButtonLink variant="secondary" href="/overlays">{t('overlays.customBack')}</ButtonLink>
      {/snippet}
    </PageHead>
    {#if data.readOnly}
      <PageNotice tone="info" role="note">{t('overlays.readOnly')}</PageNotice>
    {:else}
      <Stack gap={4}>
        {#if locked}
          <AlertBanner tone="warm" role="note" cta={{ label: t('billing.premiumGateCta'), href: '/billing?subscribe=1' }}>
            {t('overlayEditor.page.upsell')}
          </AlertBanner>
        {/if}
        <PageNotice tone="warning" visible={data.missing}>{t('overlayEditor.page.notFound')}</PageNotice>
        <ImportPanel premium={!locked} onimported={openImported} />
        {#if creating}
          <Cluster gap={3}>
            <Text size="sm" tone="muted">{t('overlayEditor.page.templates')}</Text>
            {#each templateNames as template (template)}
              <Button size="sm" variant="secondary" disabled={!creating} onclick={() => start(template)}>
                <bb-fit><span data-fit="">{t(`overlayEditor.template.${template}`)}</span></bb-fit>
              </Button>
            {/each}
          </Cluster>
        {/if}
        {#key seed.id}
          <OverlayEditor initial={seed} {media} preview={locked} onsave={save} ondirty={(value) => (dirty = value)} />
        {/key}
      </Stack>
    {/if}
  </section>
{/if}

<style>
  .page-title { min-inline-size: 0; }
</style>
