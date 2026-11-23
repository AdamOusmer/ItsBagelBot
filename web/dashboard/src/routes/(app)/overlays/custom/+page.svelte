<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { beforeNavigate, goto } from '$app/navigation';
  import { page } from '$app/state';
  import { untrack } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import OverlayEditor from '@bagel/kit/components/overlay-editor/OverlayEditor.svelte';
  import type { OverlayDocument } from '@bagel/kit/overlay-document';
  import {
    blankDocument, createOverlay, fromServed, parseSize, resizeCanvas, templateNames, type SaveOutcome
  } from '@bagel/kit/overlay-editor';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import PremiumGate from '#lib/components/shared/PremiumGate.svelte';
  import { premiumLocked } from '#lib/components/shared/premium-lock';
  import { dashboardMedia } from '#lib/overlay-media';
  import { postSave } from './save-client';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();
  const { t } = getI18n();

  const PREVIEW_URL = '/overlays/custom?preview=1';
  const OVERLAYS_URL = '/overlays';

  function starterFor(wanted: string | null | undefined): OverlayDocument {
    const template = templateNames.find((name) => name === wanted);
    if (!template) return blankDocument(t('overlayEditor.page.newName'));
    return createOverlay(template, t(`overlayEditor.template.${template}`), (kind) => t(`overlayEditor.kind.${kind}`));
  }

  function seedFor(overlay: PageData['overlay']): OverlayDocument {
    if (overlay?.document) return fromServed(overlay.document);
    const search = page.url?.searchParams;
    const starter = starterFor(search?.get('template'));
    const size = parseSize(search?.get('size'));
    return size ? resizeCanvas(starter, size) : starter;
  }

  let seed = $state.raw<OverlayDocument>(blankDocument(t('overlayEditor.page.newName')));
  let overlayId = $state(0);
  let dirty = $state(false);

  function adopt(overlay: PageData['overlay']): void {
    seed = seedFor(overlay);
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
  const discard = createDiscardGuard(() => dirty);

  beforeNavigate(({ cancel, to, type }) => {
    if (!dirty || type === 'goto') return;
    cancel();
    if (type === 'leave' || !to) return;
    const target = to.url;
    discard.guard(() => void goto(target));
  });

  function savedUrl(id: number): string {
    const query = new URLSearchParams({ id: String(id) });
    if (data.preview) query.set('preview', '1');
    return `/overlays/custom?${query}`;
  }

  async function save(doc: OverlayDocument): Promise<SaveOutcome> {
    const reply = await postSave(overlayId, doc);
    if (!reply.ok) return { ok: false, error: reply.error || t('overlayEditor.save.failed') };
    if (overlayId === 0) void goto(savedUrl(reply.id), { replaceState: true });
    overlayId = reply.id;
    return reply;
  }
</script>

<svelte:head><title>{t('overlays.customTitle')}</title></svelte:head>

{#snippet notice()}
  {#if locked}
    <AlertBanner tone="warm" role="note" cta={{ label: t('billing.premiumGateCta'), href: '/billing?subscribe=1' }}>
      {t('overlayEditor.page.upsell')}
    </AlertBanner>
  {/if}
  <PageNotice tone="warning" visible={data.missing}>{t('overlayEditor.page.notFound')}</PageNotice>
{/snippet}

{#if locked && !data.preview}
  <PremiumGate trial={{ label: t('overlayEditor.page.try'), href: PREVIEW_URL }} />
{:else if data.readOnly}
  <section class="screen active">
    <ButtonLink variant="secondary" href={OVERLAYS_URL}>{t('overlays.customBack')}</ButtonLink>
    <PageNotice tone="info" role="note">{t('overlays.readOnly')}</PageNotice>
  </section>
{:else}
  {#key seed.id}
    <OverlayEditor initial={seed} {media} preview={locked} backHref={OVERLAYS_URL} {notice} onsave={save} ondirty={(value) => (dirty = value)} />
  {/key}
  <ConfirmDialog
    open={discard.open}
    title={t('overlayEditor.page.discardTitle')}
    body={t('overlayEditor.page.discard')}
    confirmLabel={t('overlayEditor.page.discardConfirm')}
    cancelLabel={t('overlayEditor.page.discardKeep')}
    tone="danger"
    onCancel={discard.cancel}
    onConfirm={discard.confirm}
  />
{/if}
