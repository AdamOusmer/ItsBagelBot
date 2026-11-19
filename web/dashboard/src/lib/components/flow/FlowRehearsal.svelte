<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import ChatPreview from '@bagel/ui/svelte/ChatPreview.svelte';
  import RehearsedLineRow from '#lib/components/commands/RehearsedLineRow.svelte';
  import type { RehearsalStep } from '#lib/flow/rehearse';

  let { steps, focusId, title }: { steps: RehearsalStep[]; focusId?: string; title?: string } = $props();
  const { t } = getI18n();
  const visible = $derived(focusId ? steps.filter((step) => step.id === focusId) : steps);
  const lines = $derived(visible.flatMap((step) => step.chatLines ?? []));
</script>

<ChatPreview title={title ?? t('chatPreview.rehearsal')}>
  <div class="chat-lines">
    {#each lines as line, index (index)}
      <RehearsedLineRow {line} {index} seed="ItsBagelBot" />
    {/each}
  </div>
  {#each visible.filter((step) => step.missing.length) as step (step.key)}
    <p class="missing">{t('flows.test.missing', { names: step.missing.join(', ') })}</p>
  {/each}
  {#each visible.filter((step) => step.note) as step (step.key)}
    <p class="note">{step.note}</p>
  {/each}
  {#if !visible.length}
    <p class="note">{t('flows.test.notReached')}</p>
  {:else if !lines.length}
    <p class="note">{visible.at(-1)?.text}</p>
  {/if}
</ChatPreview>

<style>
  .chat-lines { display: contents; white-space: pre-line; }
  .note, .missing { margin: 0; font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
  .missing { color: var(--bb-tan); }
</style>
