<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { OverlayView } from '#lib/server/services/overlays';

  let { overlays, beforeChange }: {
    overlays: OverlayView[];
    beforeChange: () => Promise<void>;
  } = $props();

  const { t } = getI18n();
  const NEW_OVERLAY = 0;

  let links = $state<Record<number, string>>({});
  let busyId = $state<number | null>(null);
  let error = $state('');
  let copyFailedId = $state<number | null>(null);
  let deleteTarget = $state<OverlayView | null>(null);
  let deleteForm = $state<HTMLFormElement>();

  const hasPowered = $derived(overlays.some((overlay) => overlay.template === 'powered'));
  const busy = $derived(busyId !== null);
  const status = $derived(error || (copyFailedId === null ? '' : t('overlays.copyFailed')));

  function record(result: { type: string; data?: Record<string, unknown> }) {
    if (result.type === 'success' && typeof result.data?.url === 'string') {
      links = { ...links, [Number(result.data.id)]: result.data.url };
    }
    if (result.type === 'failure' && typeof result.data?.listError === 'string') error = result.data.listError;
  }

  const submitLink: SubmitFunction = async ({ cancel, formData }) => {
    if (busy) return cancel();
    busyId = Number(formData.get('id')) || NEW_OVERLAY;
    error = '';
    await beforeChange();
    return async ({ update, result }) => {
      try {
        record(result);
        await update({ reset: false });
      } finally {
        busyId = null;
      }
    };
  };

  const submitDelete: SubmitFunction = async ({ cancel, formData }) => {
    if (busy) return cancel();
    busyId = Number(formData.get('id'));
    error = '';
    await beforeChange();
    return async ({ update, result }) => {
      try {
        record(result);
        if (result.type === 'success') deleteTarget = null;
        await update({ reset: false });
      } finally {
        busyId = null;
      }
    };
  };
</script>

<section class="list" aria-labelledby="overlays-title">
  <div class="list-head">
    <Heading level={6} as="h2" id="overlays-title">
      <bb-fit><span data-fit="">{t('overlays.listTitle')}</span></bb-fit>
    </Heading>
    <div class="list-actions">
      <form method="POST" action="?/add" use:enhance={submitLink}>
        <Button variant="secondary" type="submit" busy={busyId === NEW_OVERLAY} disabled={hasPowered || busy}>
          <bb-fit class="action-label"><span data-fit="">{t('overlays.addPowered')}</span></bb-fit>
        </Button>
      </form>
      <ButtonLink variant="secondary" href="/overlays/custom">
        <bb-fit class="action-label"><span data-fit="">{t('overlays.createCustom')}</span></bb-fit>
      </ButtonLink>
    </div>
  </div>
  <div class="feedback bb-fit-box" class:visible={!!status} role="status">
    <Text as="div" size="xs" tone="danger"><bb-fit mode="block">{status}</bb-fit></Text>
  </div>
  <ul class="cards">
    {#each overlays as overlay (overlay.id)}
      {@const url = links[overlay.id]}
      <li>
        <Card as="section" class="item" aria-labelledby="overlay-{overlay.id}">
          <div class="name">
            <Heading level={6} as="h3" id="overlay-{overlay.id}">
              <bb-fit><span data-fit="">{overlay.name}</span></bb-fit>
            </Heading>
            <Badge tone={overlay.kind === 'custom' ? 'paid' : 'free'}>{t(`overlays.kind.${overlay.kind}`)}</Badge>
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
            <form method="POST" action="?/rotate" use:enhance={submitLink}>
              <input type="hidden" name="id" value={overlay.id} />
              <Button variant="secondary" type="submit" busy={busyId === overlay.id} disabled={busy}>
                <bb-fit class="action-label"><span data-fit="">{t('overlays.replace')}</span></bb-fit>
              </Button>
            </form>
            {#if overlay.kind === 'custom'}
              <Button variant="secondary" type="button" disabled={busy} onclick={() => (deleteTarget = overlay)}>
                <bb-fit class="action-label"><span data-fit="">{t('common.delete')}</span></bb-fit>
              </Button>
            {/if}
          </div>
        </Card>
      </li>
    {:else}
      <li class="empty"><Text as="p" size="sm" tone="muted">{t('overlays.listEmpty')}</Text></li>
    {/each}
  </ul>
</section>

<form class="delete-form" method="POST" action="?/delete" use:enhance={submitDelete} bind:this={deleteForm}>
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
  .list-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--bb-space-3);
  }
  .list-actions { display: flex; flex-wrap: wrap; gap: var(--bb-space-3); }
  .action-label { line-height: 1.4; }
  .feedback {
    block-size: 1.25rem;
    min-inline-size: 0;
    align-content: center;
    white-space: nowrap;
    opacity: 0;
  }
  .feedback.visible { opacity: 1; }
  .cards { display: grid; gap: var(--bb-space-3); margin: 0; padding: 0; list-style: none; }
  .list :global(.item) {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 27.5rem) auto;
    align-items: center;
    gap: var(--bb-space-4);
  }
  .name { display: flex; align-items: center; gap: var(--bb-space-3); min-inline-size: 0; }
  .link { display: grid; min-inline-size: 0; }
  .layer { grid-area: 1 / 1; align-content: center; min-inline-size: 0; opacity: 0; }
  .layer.visible { opacity: 1; }
  .item-actions { display: flex; gap: var(--bb-space-3); }
  .item-actions form { display: contents; }
  .delete-form { display: none; }
  @media (prefers-reduced-motion: no-preference) {
    .feedback, .layer { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
  @media (max-width: 760px) {
    .list :global(.item) { grid-template-columns: minmax(0, 1fr); }
  }
</style>
