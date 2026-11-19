<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import FieldError from '@bagel/ui/svelte/FieldError.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { buildJsonPath, getI18n, type ActionOk } from '@bagel/kit';
  import { postAction } from '#lib/client-actions.svelte';
  import JsonTree from '#lib/components/commands/fetches/JsonTree.svelte';

  let { url, onPick }: { url: string; onPick: (choice: { path: string; sample: string }) => void } = $props();

  const { t } = getI18n();
  const hintId = $props.id();
  const fixedHttps = (address: string): boolean => /^https:\/\/[^{}]*$/i.test(address) && URL.canParse(address);
  const eligible = $derived(fixedHttps(url));
  let loading = $state(false);
  let sample = $state('');
  let error = $state('');

  async function tryRead() {
    loading = true;
    sample = '';
    error = '';
    const form = new FormData();
    form.set('kind', 'json');
    form.set('url', url);
    form.set('path', '');
    const reply = await postAction<ActionOk & { status?: string; sample?: string }>('testfetch', form);
    loading = false;
    if (reply?.ok && reply.status === 'ok' && reply.sample) {
      sample = reply.sample;
      return;
    }
    error = reply?.error ?? t('flows.step.reply.httpGet.tryIt.error');
  }
</script>

<div class="try-it">
  <div class="controls">
    <Button variant="ghost" size="sm" disabled={!eligible} busy={loading} aria-describedby={eligible ? undefined : hintId} onclick={tryRead}>
      <bb-fit>{t('flows.step.reply.httpGet.tryIt.label')}</bb-fit>
    </Button>
    <small class="bb-field__hint bb-fit-box fixed-hint" class:faded={eligible} aria-hidden={eligible} id={hintId}>
      <bb-fit mode="block">{t('flows.step.reply.httpGet.tryIt.fixedAddress')}</bb-fit>
    </small>
  </div>
  <div class="result" class:faded={!loading && !error && !sample}>
    <div role="status">
      {#if loading}
        <Text size="xs" tone="muted">{t('flows.step.reply.httpGet.tryIt.loading')}</Text>
      {/if}
    </div>
    <FieldError message={error} />
    <JsonTree json={sample} onPick={(segments) => onPick({ path: buildJsonPath(segments), sample })} />
  </div>
</div>

<style>
  .try-it {
    margin-block-start: var(--field-gap);
  }
  .controls {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--field-gap);
  }
  .fixed-hint {
    block-size: 2.5rem;
  }
  .result {
    block-size: 8rem;
    overflow: auto;
  }
  .faded {
    opacity: 0;
  }
</style>
