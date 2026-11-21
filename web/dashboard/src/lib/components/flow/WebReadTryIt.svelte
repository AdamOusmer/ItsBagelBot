<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import '@bagel/ui/styles/elements/collapse.css';
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
  const resultOpen = $derived(loading || Boolean(error) || Boolean(sample));

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
    <div class="bb-collapse" class:is-open={!eligible} aria-hidden={eligible} inert={eligible}>
      <div><small class="bb-field__hint" id={hintId}>{t('flows.step.reply.httpGet.tryIt.fixedAddress')}</small></div>
    </div>
  </div>
  <div class="bb-collapse" class:is-open={resultOpen} aria-hidden={!resultOpen} inert={!resultOpen}>
    <div class="result">
      <div role="status">
        {#if loading}
          <Text size="xs" tone="muted">{t('flows.step.reply.httpGet.tryIt.loading')}</Text>
        {/if}
      </div>
      <FieldError message={error} />
      <JsonTree json={sample} onPick={(segments) => onPick({ path: buildJsonPath(segments), sample })} />
    </div>
  </div>
</div>

<style>
  .try-it {
    display: grid;
    row-gap: var(--field-gap);
    margin-block-start: var(--field-gap);
    --bb-collapse-gap: var(--field-gap);
  }
  .controls {
    --bb-collapse-gap: 0px;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--field-gap);
  }
  .result {
    max-block-size: 8rem;
    overflow: auto;
  }
</style>
