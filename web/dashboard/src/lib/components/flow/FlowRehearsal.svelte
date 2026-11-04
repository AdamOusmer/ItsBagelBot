<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit';
  import ChatPreview from '@bagel/ui/svelte/ChatPreview.svelte';
  import ChatBotName from '#lib/components/commands/ChatBotName.svelte';
  import RehearsedLineRow from '#lib/components/commands/RehearsedLineRow.svelte';
  import RehearsedSegments from '#lib/components/commands/RehearsedSegments.svelte';
  import type { RehearsalStep } from '#lib/flow/rehearse';

  let { steps, focusId, title }: { steps: RehearsalStep[]; focusId?: string; title?: string } = $props();

  const { t } = getI18n();

  let typing = $state(false);
  let hovered = $state(false);

  const seed = $derived((page.data.displayName as string | undefined) ?? 'ItsBagelBot');
  const unknownTitle = $derived(t('chatPreview.unknownVar'));
  const inChat = $derived(steps.filter((s) => s.kind === 'viewer' || s.kind === 'event' || s.kind === 'bot'));
  const focusText = $derived(JSON.stringify(steps.find((s) => s.id === focusId)));

  let settle: ReturnType<typeof setTimeout> | undefined;
  let lastText: string | undefined;
  let lastFocus: string | undefined;
  $effect(() => {
    const text = focusText;
    const changed = lastFocus === focusId && lastText !== undefined && lastText !== text;
    lastText = text;
    lastFocus = focusId;
    if (!changed) return;
    typing = true;
    clearTimeout(settle);
    settle = setTimeout(() => (typing = false), 550);
    return () => clearTimeout(settle);
  });
</script>

<ChatPreview
  title={title ?? t('chatPreview.rehearsal')}
  label={t('chatPreview.ariaPreview')}
  onpointerenter={() => (hovered = true)}
  onpointerleave={() => (hovered = false)}
>
  {#each inChat as step (step.id)}
    {#if step.kind === 'viewer'}
      <div class="bb-chat__line">
        <span class="bb-chat__name bb-chat__name--viewer">{step.name}</span>
        <span class="bb-chat__msg bb-chat__msg--cmd">{step.text}</span>
      </div>
    {:else if step.kind === 'event'}
      <div class="bb-chat__line">
        <span class="bb-chat__msg event"><RehearsedSegments segments={step.segments} {unknownTitle} /></span>
      </div>
    {:else if step.kind === 'bot' && typing && step.id === focusId}
      <div class="bb-chat__line">
        <ChatBotName {seed} active />
        <span class="bb-chat__msg bb-chat__msg--typing" aria-label={t('chatPreview.ariaTyping')}>
          <span class="bb-drawline" aria-hidden="true"></span>
        </span>
      </div>
    {:else if step.kind === 'bot' && step.lines.length === 0}
      <div class="bb-chat__line">
        <ChatBotName {seed} active={hovered} />
        <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.nothingToSay')}</span>
      </div>
    {:else if step.kind === 'bot'}
      {#each step.lines as line, i (i)}
        <RehearsedLineRow {line} index={i} {seed} active={hovered} />
      {/each}
    {/if}
  {:else}
    <div class="bb-chat__line">
      <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.nothingToSay')}</span>
    </div>
  {/each}
</ChatPreview>

<style>
  .event {
    font-style: italic;
  }
</style>
