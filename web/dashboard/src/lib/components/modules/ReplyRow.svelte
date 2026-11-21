<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/row-body.css';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import {
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
    disabled = false,
    voice,
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
    disabled?: boolean;
    voice?: { checked: boolean; allowed: boolean; status: SaveState; onToggle: (checked: boolean) => void };
    onExpand: () => void;
    onToggle?: () => void;
  } = $props();

  const replyLabel = $derived(tModuleReplyPart(t, moduleId, reply, 'label'));
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const preview = $derived(namespaceReplyTemplate(moduleId, reply, message.trim() ? message : tModuleReplyDefault(t, moduleId, reply)));
</script>

<div class="row-wrap" class:flash-save={status === 'saved'}>
  <ManagementRow
    selected={expanded}
    {expanded}
    disabled={enabled === false}
    aria-disabled={disabled ? 'true' : undefined}
    onSelect={onExpand}
  >
    {#snippet primary()}
      <span class="prow bb-row-body bb-row-body--stack">
        {#if idx}<span class="bb-row-body__idx" aria-hidden="true"><Label mono as="span">{idx}</Label></span>{/if}
        <span class="cmd">
          <Heading level={6} as="span">{replyLabel}</Heading>
          <Text as="span" size="xs" tone="muted" truncate>{preview}</Text>
        </span>
        <span class="state"><SaveStatus state={status} /></span>
      </span>
    {/snippet}
    {#snippet actions()}
      {#if enabled !== undefined}
        <Switch checked={enabled} label={t('modules.toggleAria', { label: replyLabel })} disabled={disabled} onCheckedChange={() => onToggle?.()} />
      {:else}
        <span class="mini-spacer" aria-hidden="true"></span>
      {/if}
    {/snippet}
  </ManagementRow>
  {#if voice}
    {@const voiceLabel = t('modules.readPremiumVoice')}
    <ManagementRow selectable={false}>
      {#snippet primary()}
        <span class="voice-copy">
          <Text as="span" size="xs" class="bb-fit-box">
            <bb-fit><span data-fit="">{voiceLabel}</span></bb-fit>
          </Text>
          <span class="voice-tags">
            <Tag tone="alpha">{t('nav.beta')}</Tag>
            {#if !voice.allowed}<Tag tone="quiet">{t('billing.premium')}</Tag>{/if}
          </span>
        </span>
      {/snippet}
      {#snippet actions()}
        <span class="voice-status"><SaveStatus state={voice.status} compact /></span>
        <Switch
          checked={voice.allowed && voice.checked}
          label={`${voiceLabel} (${replyLabel})`}
          disabled={disabled || !voice.allowed}
          busy={voice.status === 'saving'}
          onCheckedChange={voice.onToggle}
        />
      {/snippet}
    </ManagementRow>
  {/if}
</div>

<style>
  .prow { --row-cols: 28px minmax(0, 1fr) auto; }

  .cmd { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .state { min-width: 0; }
  .mini-spacer { width: 38px; }
  .voice-copy { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-1); }
  .voice-tags { display: inline-flex; gap: var(--bb-space-1); }
  .voice-status { width: 20px; }

  @media (max-width: 760px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; }
    .state { display: none; }
  }
</style>
