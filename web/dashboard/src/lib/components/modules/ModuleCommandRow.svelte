<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Code from '@bagel/ui/svelte/Code.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, tModuleCommandSummary, tPermBadge, type ModuleCommandInfo, type Perm } from '@bagel/kit';

  const { t } = getI18n();

  const EMPTY_SET: ReadonlySet<string> = new Set();

  let {
    moduleId,
    command,
    index,
    role = undefined,
    expanded = false,
    onExpand = undefined,
    triggers = [] as readonly string[],
    disabledTriggers = EMPTY_SET,
    parentOff = false,
    locked = false,
    status = 'idle' as SaveState,
    onToggleTrigger = undefined,
    onToggleAll = undefined
  }: {
    moduleId: string;
    command: ModuleCommandInfo;
    index: number;
    role?: Perm;
    expanded?: boolean;
    onExpand?: () => void;
    triggers?: readonly string[];
    disabledTriggers?: ReadonlySet<string>;
    parentOff?: boolean;
    locked?: boolean;
    status?: SaveState;
    onToggleTrigger?: (trigger: string, enabled: boolean) => void;
    onToggleAll?: (triggers: readonly string[], enabled: boolean) => void;
  } = $props();

  const idx = $derived(String(index).padStart(2, '0'));
  const anyOn = $derived(triggers.length > 0 ? triggers.some((tr) => !disabledTriggers.has(tr)) : !parentOff);
  const canToggleAll = $derived(triggers.length > 0 && !!onToggleAll);
  const showChips = $derived(triggers.length > 1 && !!onToggleTrigger);
</script>

{#snippet toggleActions()}
  <SaveStatus state={status} />
  <Switch
    checked={anyOn}
    label={t('modules.commandToggleAria', { trigger: command.trigger })}
    disabled={locked}
    onCheckedChange={(checked) => onToggleAll?.(triggers, checked)}
  />
{/snippet}

<div class="row-wrap">
  <ManagementRow
    selectable={!!onExpand}
    selected={expanded}
    {expanded}
    disabled={!anyOn}
    onSelect={onExpand}
    actions={canToggleAll ? toggleActions : undefined}
  >
    {#snippet primary()}
      <span class="crow">
        <span class="idx" aria-hidden="true"><Label mono as="span">{idx}</Label></span>
        <span class="cmd">
          <span><Code>{command.trigger}</Code></span>
          <Text as="span" size="xs" tone="muted" truncate>{tModuleCommandSummary(t, moduleId, command)}</Text>
        </span>
        {#if role}
          <Tag tone="bare">{tPermBadge(t, role)}</Tag>
        {:else if command.perm === 'mod'}
          <Tag tone="bare">{t('modules.permMods')}</Tag>
        {:else if command.perm === 'lead_mod'}
          <Tag tone="bare">{t('modules.permLeadMods')}</Tag>
        {:else}
          <span class="mini-spacer" aria-hidden="true"></span>
        {/if}
      </span>
    {/snippet}
  </ManagementRow>

  {#if showChips}
    <div class="triggers" role="group" aria-label={t('modules.triggersGroupAria', { trigger: command.trigger })}>
      {#each triggers as trigger (trigger)}
        {@const on = !disabledTriggers.has(trigger)}
        <Chip
          pressed={on}
          tone={on ? undefined : 'muted'}
          disabled={locked}
          aria-label={t('modules.triggerToggleAria', { trigger: `!${trigger}` })}
          onclick={() => onToggleTrigger?.(trigger, !on)}
        >
          <StatusDot tone={on ? 'success' : 'neutral'} flat />!{trigger}
        </Chip>
      {/each}
    </div>
  {/if}
</div>

<style>
  .row-wrap { display: block; }
  .crow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { opacity: 0.55; }

  .cmd { display: flex; flex-direction: column; gap: 3px; min-width: 0; }

  .mini-spacer { width: 0; }

  .triggers {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    padding: 8px 14px 10px 56px;
    border-bottom: 1px solid var(--bb-border);
  }

  @media (max-width: 760px) {
    .crow { grid-template-columns: minmax(0, 1fr) auto; }
    .idx { display: none; }
    .triggers { padding: 6px 14px 10px; }
  }
</style>
