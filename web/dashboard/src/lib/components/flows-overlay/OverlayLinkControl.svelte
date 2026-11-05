<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit';

  const ENDPOINT = '/settings/overlay-token';

  const { t } = getI18n();

  let configured = $state<boolean | null>(null);
  let freshUrl = $state('');
  let confirming = $state(false);
  let busy = $state(false);
  let failed = $state(false);

  onMount(async () => {
    try {
      const res = await fetch(ENDPOINT);
      if (!res.ok) throw new Error(String(res.status));
      configured = ((await res.json()) as { configured: boolean }).configured;
    } catch {
      failed = true;
    }
  });

  async function makeLink() {
    busy = true;
    failed = false;
    try {
      const res = await fetch(ENDPOINT, { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{}' });
      if (!res.ok) throw new Error(String(res.status));
      freshUrl = ((await res.json()) as { url: string }).url;
      configured = true;
      confirming = false;
    } catch {
      failed = true;
    } finally {
      busy = false;
    }
  }
</script>

<section class="overlay-link" aria-label={t('flows.overlay.title')}>
  <Text as="p" size="sm">{t('flows.overlay.title')}</Text>
  <Text as="p" size="sm" tone="muted">{t('flows.overlay.body')}</Text>

  {#if freshUrl}
    <Text as="p" size="xs" tone="muted">{t('flows.overlay.copyNow')}</Text>
    <CopySurface text={freshUrl} variant="row" label={t('flows.overlay.copyLabel')} />
  {:else if configured !== null}
    <Text as="p" size="xs" tone="muted">{configured ? t('flows.overlay.active') : t('flows.overlay.none')}</Text>
  {/if}

  {#if confirming}
    <Text as="p" size="xs" tone="muted">{t('flows.overlay.rotateWarning')}</Text>
    <div class="overlay-link-actions">
      <Button size="sm" {busy} onclick={makeLink}>{t('flows.overlay.rotateConfirm')}</Button>
      <Button size="sm" variant="ghost" onclick={() => (confirming = false)}>{t('flows.overlay.rotateCancel')}</Button>
    </div>
  {:else if configured}
    <div class="overlay-link-actions">
      <Button size="sm" variant="ghost" onclick={() => (confirming = true)}>{t('flows.overlay.rotate')}</Button>
    </div>
  {:else if configured === false}
    <div class="overlay-link-actions">
      <Button size="sm" {busy} onclick={makeLink}>{t('flows.overlay.create')}</Button>
    </div>
  {/if}

  {#if failed}
    <Text as="p" size="xs" tone="muted" role="alert">{t('flows.overlay.failed')}</Text>
  {/if}
</section>

<style>
  .overlay-link {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .overlay-link-actions {
    display: flex;
    gap: 8px;
  }
</style>
