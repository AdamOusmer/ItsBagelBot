<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import ChatPreview from '@bagel/ui/svelte/ChatPreview.svelte';
  import ChatBotName from '#lib/components/commands/ChatBotName.svelte';
  import type { RehearsalStep } from '#lib/flow/rehearse';

  let { steps, focusId, title }: { steps: RehearsalStep[]; focusId?: string; title?: string } = $props();
  const { t } = getI18n();
  const visible = $derived(focusId ? steps.filter((step) => step.id === focusId) : steps);
  const lines = $derived(visible.flatMap((step) => step.lines ?? []));
</script>

<ChatPreview title={title ?? t('chatPreview.rehearsal')}>
  {#each lines as line, index (index)}
    <div class="bb-chat__line">
      <ChatBotName seed="ItsBagelBot" />
      <span class="bb-chat__msg">{line}</span>
    </div>
  {/each}
  {#each visible.filter((step) => step.missing.length) as step (step.key)}
    <p class="missing">{t('flows.test.missing', { names: step.missing.join(', ') })}</p>
  {/each}
  {#if !visible.length}
    <p class="note">{t('flows.test.notReached')}</p>
  {:else if !lines.length}
    <p class="note">{visible.at(-1)?.text}</p>
  {/if}
</ChatPreview>

<style>
  .note, .missing { margin: 0; font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
  .missing { color: var(--bb-tan); }
</style>
