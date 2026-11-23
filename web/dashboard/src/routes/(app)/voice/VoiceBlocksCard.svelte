<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import '@bagel/ui/styles/elements/collapse.css';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { BlockedViewer } from '#lib/server/services/voice';

  let { manual, error }: { manual: BlockedViewer[]; error?: string } = $props();

  const { t } = getI18n();
  let name = $state('');
  let busy = $state(false);

  const rows = $derived(manual.map((viewer) => ({
    id: viewer.viewer_id,
    label: viewer.viewer_login || t('voice.blocks.viewerId', { id: viewer.viewer_id })
  })));

  const submit: SubmitFunction = ({ cancel }) => {
    if (busy) return cancel();
    busy = true;
    return async ({ update, result }) => {
      try {
        if (result.type === 'success') name = '';
        await update({ reset: false });
      } finally {
        busy = false;
      }
    };
  };
</script>

<Card as="section" aria-labelledby="voice-blocks-title">
  <div class="blocks">
    <Heading level={6} as="h2" id="voice-blocks-title">
      <bb-fit><span data-fit="">{t('voice.blocks.title')}</span></bb-fit>
    </Heading>
    <Text as="div" size="sm" tone="muted"><bb-fit mode="block">{t('voice.blocks.hint')}</bb-fit></Text>
    <form class="add" method="POST" action="?/blockViewer" use:enhance={submit}>
      <Input fill name="viewer" bind:value={name} maxlength={32} required autocomplete="off"
        placeholder={t('voice.blocks.namePlaceholder')} aria-label={t('voice.blocks.nameLabel')}
        invalid={!!error} aria-invalid={error ? 'true' : undefined} />
      <Button variant="secondary" size="sm" type="submit" {busy} disabled={busy}>
        <bb-fit class="label"><span data-fit="">{t('voice.blocks.add')}</span></bb-fit>
      </Button>
    </form>
    <div class="bb-collapse" class:is-open={!!error} aria-hidden={!error} role="status">
      <div>
        <Text as="div" size="sm" tone="danger"><bb-fit mode="block">{error ?? ''}</bb-fit></Text>
      </div>
    </div>
    <ul class="rows" aria-labelledby="voice-blocks-title">
      {#each rows as row (row.id)}
        <li class="row">
          <div class="who">
            <Text as="div" size="sm"><bb-fit><span data-fit="">{row.label}</span></bb-fit></Text>
          </div>
          <div class="tag">
            <Tag tone="banned" bare contain><bb-fit><span data-fit="">{t('voice.blocks.tagManual')}</span></bb-fit></Tag>
          </div>
          <form class="act" method="POST" action="?/unblockViewer" use:enhance={submit}>
            <input type="hidden" name="viewer_id" value={row.id} />
            <Button variant="secondary" size="sm" type="submit" disabled={busy}>
              <bb-fit class="label"><span data-fit="">{t('voice.blocks.unblock')}</span></bb-fit>
            </Button>
          </form>
        </li>
      {:else}
        <li class="empty">
          <Text as="div" size="sm" tone="muted"><bb-fit><span data-fit="">{t('voice.blocks.empty')}</span></bb-fit></Text>
        </li>
      {/each}
    </ul>
  </div>
</Card>

<style>
  .blocks {
    --bb-collapse-gap: var(--bb-space-2);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }
  .add {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 7rem;
    gap: var(--bb-space-2);
    align-items: center;
    --btn-w: 100%;
    --btn-justify: center;
  }
  .rows { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--bb-space-2); }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 6rem 7rem;
    gap: var(--bb-space-2);
    align-items: center;
    --btn-w: 100%;
    --btn-justify: center;
  }
  .who, .tag { min-inline-size: 0; }
  .label { line-height: 1.4; }
</style>
