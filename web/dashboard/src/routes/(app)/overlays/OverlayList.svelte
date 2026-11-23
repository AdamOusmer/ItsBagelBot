<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { tick } from 'svelte';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { OverlayView } from '#lib/server/services/overlays';

  let { overlays, busy = $bindable(false), onCreate }: {
    overlays: OverlayView[];
    busy?: boolean;
    onCreate: () => void;
  } = $props();

  const { t } = getI18n();

  let links = $state<Record<number, string>>({});
  let busyId = $state<number | null>(null);
  let error = $state('');
  let copyFailedId = $state<number | null>(null);
  let deleteTarget = $state<OverlayView | null>(null);
  let deleteForm = $state<HTMLFormElement>();
  let rotateForm = $state<HTMLFormElement>();
  let rotateId = $state<number | ''>('');
  let menuTarget = $state<OverlayView | null>(null);
  let menuAnchor = $state<HTMLElement>();

  const status = $derived(error || (copyFailedId === null ? '' : t('overlays.copyFailed')));

  $effect(() => {
    busy = busyId !== null;
  });

  function toggleMenu(overlay: OverlayView, event: MouseEvent) {
    if (menuTarget?.id === overlay.id) {
      menuTarget = null;
      return;
    }
    menuAnchor = event.currentTarget as HTMLElement;
    menuTarget = overlay;
  }

  async function replaceLink() {
    if (!menuTarget) return;
    rotateId = menuTarget.id;
    menuTarget = null;
    await tick();
    rotateForm?.requestSubmit();
  }

  function askDelete() {
    deleteTarget = menuTarget;
    menuTarget = null;
  }

  function record(result: { type: string; data?: Record<string, unknown> }) {
    if (result.type === 'success' && typeof result.data?.url === 'string') {
      links = { ...links, [Number(result.data.id)]: result.data.url };
    }
    if (result.type === 'failure' && typeof result.data?.listError === 'string') error = result.data.listError;
  }

  const submit: SubmitFunction = async ({ cancel, formData }) => {
    if (busy) return cancel();
    busyId = Number(formData.get('id'));
    error = '';
    return async ({ update, result }) => {
      try {
        record(result);
        if (result.type === 'success' && result.data?.deleted) deleteTarget = null;
        await update({ reset: false });
      } finally {
        busyId = null;
      }
    };
  };
</script>

<section class="list" aria-label={t('overlays.yours')}>
  <Heading level={5} as="h2"><bb-fit><span data-fit="">{t('overlays.yours')}</span></bb-fit></Heading>
  <div class="feedback bb-fit-box" class:visible={!!status} role="status">
    <Text as="div" size="xs" tone="danger"><bb-fit mode="block">{status}</bb-fit></Text>
  </div>
  <ul class="cards">
    {#each overlays as overlay (overlay.id)}
      {@const url = links[overlay.id]}
      <li>
        <Card as="section" flush class="item" aria-labelledby="overlay-{overlay.id}" aria-busy={busyId === overlay.id}>
          <div class="thumb" aria-hidden="true">
            <span class="thumb-icon"><Icon name="overlay" size={32} /></span>
          </div>
          <div class="body">
            <div class="name">
              <Heading level={6} as="h3" id="overlay-{overlay.id}">
                <bb-fit><span data-fit="">{overlay.name}</span></bb-fit>
              </Heading>
              <Badge tone="paid">{t('overlays.kind.custom')}</Badge>
            </div>
            <div class="link">
              <div class="layer" class:visible={!url} aria-hidden={!!url}>
                <Text as="div" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.linkHidden')}</bb-fit></Text>
              </div>
              <div class="layer" class:visible={!!url} inert={!url}>
                {#if url}
                  <CopySurface variant="well" text={url} aria-label={t('overlays.link')} legacyFallback
                    onCopy={(copied) => (copyFailedId = copied ? null : overlay.id)} />
                {/if}
              </div>
            </div>
            <div class="item-actions">
              <IconButton
                label={t('overlays.menuFor', { name: overlay.name })}
                aria-haspopup="dialog"
                aria-expanded={menuTarget?.id === overlay.id}
                disabled={busy}
                onclick={(event) => toggleMenu(overlay, event)}
              >
                <span aria-hidden="true">…</span>
              </IconButton>
              <div class="edit">
                <ButtonLink variant="secondary" href="/overlays/custom?id={overlay.id}" aria-label={t('overlays.editNamed', { name: overlay.name })}>
                  <bb-fit class="action-label"><span data-fit="">{t('overlays.edit')}</span></bb-fit>
                </ButtonLink>
              </div>
            </div>
          </div>
        </Card>
      </li>
    {:else}
      <li>
        <Card as="button" type="button" dashed hover class="tile-card" style="--bb-cursor: quiet" onclick={onCreate}>
          <IconTile size="lg"><Icon name="plus" size={20} /></IconTile>
          <span class="tile-title"><bb-fit><span data-fit="">{t('overlays.emptyTitle')}</span></bb-fit></span>
          <span class="tile-body bb-fit-box"><Text as="span" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.emptyBody')}</bb-fit></Text></span>
        </Card>
      </li>
    {/each}
  </ul>
</section>

<PickerPanel open={menuTarget !== null} anchor={menuAnchor} label={t('overlays.menuTitle')} width={220} maxHeight={140} placement="below" onClose={() => (menuTarget = null)}>
  <PickerOption label={t('overlays.replace')} onclick={replaceLink} />
  <PickerOption label={t('common.delete')} onclick={askDelete} />
</PickerPanel>

<form class="hidden-form" method="POST" action="?/rotate" use:enhance={submit} bind:this={rotateForm}>
  <input type="hidden" name="id" value={rotateId} />
</form>

<form class="hidden-form" method="POST" action="?/delete" use:enhance={submit} bind:this={deleteForm}>
  <input type="hidden" name="id" value={deleteTarget?.id ?? ''} />
</form>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('overlays.deleteTitle', { name: deleteTarget?.name ?? '' })}
  body={t('overlays.deleteBody')}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  tone="danger"
  busy={busy}
  onConfirm={() => deleteForm?.requestSubmit()}
  onCancel={() => (deleteTarget = null)}
/>

<style>
  .list { display: grid; gap: var(--bb-space-3); margin-block-end: var(--bb-space-5); }
  .action-label { line-height: 1.4; }
  .feedback {
    block-size: 1.25rem;
    min-inline-size: 0;
    align-content: center;
    white-space: nowrap;
    opacity: 0;
  }
  .feedback.visible { opacity: 1; }
  .cards {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--bb-space-4);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .cards > li { display: grid; min-inline-size: 0; }
  .list :global(.item) { display: grid; grid-template-rows: auto 1fr; block-size: 100%; }
  .thumb {
    display: grid;
    place-items: center;
    aspect-ratio: 16 / 9;
    background: linear-gradient(150deg, var(--bb-elev-2), var(--bb-bg-1));
    color: var(--bb-white);
  }
  .thumb-icon { display: grid; opacity: 0.55; }
  .body { display: grid; grid-template-rows: auto 2.75rem auto; gap: var(--bb-space-3); padding: var(--bb-space-4); }
  .name { display: flex; align-items: center; justify-content: space-between; gap: var(--bb-space-3); min-inline-size: 0; }
  .link { display: grid; min-inline-size: 0; }
  .layer { grid-area: 1 / 1; align-content: center; min-inline-size: 0; opacity: 0; }
  .layer.visible { opacity: 1; }
  .item-actions { display: flex; align-items: center; justify-content: space-between; gap: var(--bb-space-3); }
  .edit { min-inline-size: 5.5rem; }
  .list :global(.tile-card) {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--bb-space-2);
    inline-size: 100%;
    block-size: 100%;
    min-block-size: 12rem;
    padding: var(--bb-space-4);
    color: inherit;
    font: inherit;
    text-align: center;
    cursor: pointer;
  }
  .list :global(.tile-card:disabled) { cursor: default; }
  .tile-title { display: block; inline-size: 100%; min-inline-size: 0; font-weight: 700; line-height: 1.4; }
  .tile-body { display: block; inline-size: 100%; min-inline-size: 0; block-size: 2.5em; line-height: 1.25; }
  .hidden-form { display: none; }
  @media (prefers-reduced-motion: no-preference) {
    .feedback, .layer { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
  @media (max-width: 1100px) {
    .cards { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-width: 640px) {
    .cards { grid-template-columns: minmax(0, 1fr); }
  }
</style>
