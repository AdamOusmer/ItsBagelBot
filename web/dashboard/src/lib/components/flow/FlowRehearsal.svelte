<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import ChatPreview from '@bagel/ui/svelte/ChatPreview.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import RehearsedLineRow from '#lib/components/commands/RehearsedLineRow.svelte';
  import type { RehearsalStep } from '#lib/flow/rehearse';

  let { steps, focusId, summary }: { steps: RehearsalStep[]; focusId: string; summary: string } = $props();
  const { t } = getI18n();
  const visible = $derived(steps.filter((step) => step.id === focusId));
  const lines = $derived(visible.flatMap((step) => step.chatLines ?? []));
  const notes = $derived(visible.flatMap((step) => [
    ...(step.missing.length ? [{ key: `${step.key}:missing`, text: t('flows.test.missing', { names: step.missing.join(', ') }), missing: true }] : []),
    ...(step.note ? [{ key: `${step.key}:note`, text: step.note, missing: false }] : [])
  ]));
</script>

<section class="preview" aria-label={t('flows.board.whatHappens')}>
  <Label as="span" class="preview-label bb-fit-box"><bb-fit>{t('flows.board.whatHappens')}</bb-fit></Label>
  <Text size="sm" tone="muted-light" class="preview-summary bb-fit-box"><bb-fit mode="block">{summary}</bb-fit></Text>
  {#if lines.length}
    <ChatPreview title={t('chatPreview.rehearsal')}>
      <div class="chat-lines">
        {#each lines as line, index (index)}
          <RehearsedLineRow {line} {index} seed="ItsBagelBot" />
        {/each}
      </div>
    </ChatPreview>
  {/if}
  {#each notes as note (note.key)}
    <Text size="sm" tone={note.missing ? 'warning' : 'muted-light'} class="preview-note">{note.text}</Text>
  {/each}
</section>

<style>
  .preview {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    padding: var(--bb-space-3);
    border-radius: var(--bb-radius-md);
    background: var(--bb-inset-bg);
    box-shadow: var(--bb-depth-0);
  }
  .preview :global(.preview-label) { height: var(--bb-space-4); line-height: 1.34; }
  .preview :global(.preview-summary) {
    height: calc(var(--bb-space-5) + var(--bb-space-4));
    margin: 0;
    overflow-wrap: anywhere;
  }
  .preview :global(.preview-note) { margin: 0; overflow-wrap: anywhere; }
  .chat-lines { display: contents; white-space: pre-line; }
</style>
