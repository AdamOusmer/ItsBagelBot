<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import {
    Heading,
    Label,
    ManagementRow,
    SaveStatus,
    Switch,
    Text,
    getI18n,
    namespaceReplyTemplate,
    tModuleReplyDefault,
    tModuleReplyPart,
    type ModuleReply
  } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';

  const { t } = getI18n();

  let {
    moduleId,
    reply,
    message = '',
    index = undefined as number | undefined,
    status = 'idle' as SaveState,
    expanded = false,
    enabled = undefined as boolean | undefined,
    onExpand,
    onToggle
  }: {
    moduleId: string;
    reply: ModuleReply;
    message?: string;
    index?: number;
    status?: SaveState;
    expanded?: boolean;
    enabled?: boolean;
    onExpand: () => void;
    onToggle?: () => void;
  } = $props();

  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const preview = $derived(namespaceReplyTemplate(moduleId, reply, message.trim() ? message : tModuleReplyDefault(t, moduleId, reply)));
</script>

<div class="row-wrap" class:flash-save={status === 'saved'}>
  <ManagementRow
    selected={expanded}
    {expanded}
    disabled={enabled === false}
    onselect={onExpand}
  >
    {#snippet primary()}
      <span class="prow">
        {#if idx}<span class="idx" aria-hidden="true"><Label mono as="span">{idx}</Label></span>{/if}
        <span class="cmd">
          <Heading level={6} as="span">{tModuleReplyPart(t, moduleId, reply, 'label')}</Heading>
          <Text as="span" size="xs" tone="muted" truncate>{preview}</Text>
        </span>
        <span class="state"><SaveStatus state={status} /></span>
      </span>
    {/snippet}
    {#snippet actions()}
      {#if enabled !== undefined}
        <Switch checked={enabled} label={t('modules.toggleAria', { label: tModuleReplyPart(t, moduleId, reply, 'label') })} onchange={() => onToggle?.()} />
      {:else}
        <span class="mini-spacer" aria-hidden="true"></span>
      {/if}
    {/snippet}
  </ManagementRow>
</div>

<style>
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { opacity: 0.55; }

  .cmd { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .state { min-width: 0; }
  .mini-spacer { width: 38px; }

  @media (max-width: 760px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; }
    .idx { display: none; }
    .state { display: none; }
  }
</style>
