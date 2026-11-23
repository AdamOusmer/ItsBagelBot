<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { goto } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { OverlayView } from '#lib/server/services/overlays';
  import ReplaceLink from './ReplaceLink.svelte';

  let { powered }: { powered: OverlayView | undefined } = $props();

  const { t } = getI18n();
  const green = { head: '#8cc4b2', eye: '#111113' };
  const SETTINGS = '/overlays/powered';

  let url = $state('');
  let error = $state('');
  let adding = $state(false);
  let replacing = $state(false);

  const added = $derived(powered !== undefined);
  const busy = $derived(adding || replacing);

  async function settleAdd(result: { type: string; data?: Record<string, unknown> }) {
    if (result.type !== 'success') {
      error = typeof result.data?.listError === 'string' ? result.data.listError : t('overlays.listFailed');
      return;
    }
    const poweredLink = typeof result.data?.url === 'string' ? result.data.url : undefined;
    await goto(SETTINGS, { state: { poweredLink } });
  }

  const submitAdd: SubmitFunction = async ({ cancel }) => {
    if (busy) return cancel();
    adding = true;
    error = '';
    return async ({ result }) => {
      try {
        await settleAdd(result);
      } finally {
        adding = false;
      }
    };
  };

  function linked(next: string) {
    error = '';
    url = next;
  }
</script>

<Card as="section" class="powered-banner" aria-label={t('overlays.banner.title')}>
  <span class="mascot"><Bolota name="oven_fresh" size={48} palette={green} motionState="idle" active gate plate={false} /></span>
  <span class="copy">
    <span class="title">
      <Text as="span" size="sm" class="fit-line"><strong><bb-fit><span data-fit="">{t('overlays.banner.title')}</span></bb-fit></strong></Text>
      <Badge tone="free">{t('overlays.kind.system')}</Badge>
    </span>
    <span class="detail">
      <span class="layer" class:visible={!url} aria-hidden={!!url}>
        <Text as="span" size="xs" tone={error ? 'danger' : 'muted'} class="fit-line" role="status"><bb-fit><span data-fit="">{error || t('overlays.banner.line')}</span></bb-fit></Text>
      </span>
      <span class="layer" class:visible={!!url} inert={!url}>
        {#if url}
          <CopySurface variant="well" text={url} aria-label={t('overlays.link')} legacyFallback
            onCopy={(copied) => (error = copied ? '' : t('overlays.copyFailed'))} />
        {/if}
      </span>
    </span>
  </span>
  <span class="actions">
    <span class="slot" class:off={added} inert={added}>
      <form method="POST" action="?/add" use:enhance={submitAdd}>
        <Button type="submit" size="sm" busy={adding}>
          <bb-fit><span data-fit="">{t('overlays.banner.add')}</span></bb-fit>
        </Button>
      </form>
    </span>
    <span class="slot" class:off={!added} inert={!added}>
      <ReplaceLink id={powered?.id ?? 0} bind:busy={replacing} onLink={linked} onError={(message) => (error = message)} />
      <ButtonLink href={SETTINGS} size="sm" variant="secondary">
        <bb-fit><span data-fit="">{t('overlays.banner.customize')}</span></bb-fit>
      </ButtonLink>
    </span>
  </span>
</Card>

<style>
  :global(.powered-banner) {
    --card-bg:
      radial-gradient(60% 160% at 0% 50%, rgba(var(--bb-green-glow-rgb), 0.18), transparent 70%),
      linear-gradient(90deg, rgba(var(--bb-green-rgb), 0.12), transparent 60%),
      var(--bb-surface-raised);
    display: flex;
    align-items: center;
    gap: var(--bb-space-4);
    padding: var(--bb-space-3) var(--bb-space-4);
    margin-block-end: var(--bb-space-5);
  }
  .mascot { flex: none; inline-size: 48px; block-size: 48px; }
  .copy { display: grid; grid-template-rows: 1.5rem 2rem; gap: 2px; flex: 1; min-inline-size: 0; align-content: center; }
  .title { display: flex; align-items: center; gap: var(--bb-space-3); min-inline-size: 0; }
  .detail { display: grid; min-inline-size: 0; align-items: center; }
  .layer { grid-area: 1 / 1; display: grid; align-items: center; min-inline-size: 0; opacity: 0; }
  .layer.visible { opacity: 1; }
  .copy :global(.fit-line) { display: block; min-inline-size: 0; overflow: hidden; }
  .actions { display: grid; flex: none; justify-items: end; }
  .slot { grid-area: 1 / 1; display: flex; align-items: center; gap: var(--bb-space-2); }
  .slot.off { opacity: 0; visibility: hidden; }
  @media (prefers-reduced-motion: no-preference) {
    .layer, .slot { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
  @media (max-width: 760px) {
    :global(.powered-banner) { flex-wrap: wrap; }
    .actions { flex: 1 0 100%; justify-items: stretch; }
    .slot { justify-content: flex-end; }
  }
</style>
