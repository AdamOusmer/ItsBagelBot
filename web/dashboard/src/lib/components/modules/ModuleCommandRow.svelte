<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, tModuleCommandSummary, tPermBadge, type ModuleCommandInfo, type Perm } from '@bagel/kit';

  const { t } = getI18n();

  const EMPTY_SET: ReadonlySet<string> = new Set();

  let {
    moduleId,
    command,
    role = undefined,
    expanded = false,
    onExpand = undefined,
    triggers = [] as readonly string[],
    disabledTriggers = EMPTY_SET,
    parentOff = false,
    replyOff = false,
    locked = false,
    status = 'idle' as SaveState,
    onToggleTrigger = undefined,
    onToggleAll = undefined
  }: {
    moduleId: string;
    command: ModuleCommandInfo;
    role?: Perm;
    expanded?: boolean;
    onExpand?: () => void;
    triggers?: readonly string[];
    disabledTriggers?: ReadonlySet<string>;
    parentOff?: boolean;
    replyOff?: boolean;
    locked?: boolean;
    status?: SaveState;
    onToggleTrigger?: (trigger: string, enabled: boolean) => void;
    onToggleAll?: (triggers: readonly string[], enabled: boolean) => void;
  } = $props();

  const anyOn = $derived(triggers.length > 0 ? triggers.some((tr) => !disabledTriggers.has(tr)) : !parentOff);
  const hasToggles = $derived(triggers.length > 0 && !!onToggleAll);
  const ownsSwitch = $derived(hasToggles && !command.enableKey);
  const showChips = $derived(triggers.length > 1 && !!onToggleTrigger);
  const headToggle = $derived(showChips && !onExpand);
  const toggles = $derived(showChips ? triggers.slice(headToggle ? 1 : 0) : []);
  const staticAliases = $derived(showChips ? [] : (command.aliases ?? []));
  const permLabel = $derived(role ? tPermBadge(t, role) : fallbackPerm(command.perm));

  function fallbackPerm(perm: ModuleCommandInfo['perm']): string {
    if (perm === 'mod') return t('modules.permMods');
    if (perm === 'lead_mod') return t('modules.permLeadMods');
    return '';
  }

  function isOn(trigger: string): boolean {
    return !disabledTriggers.has(trigger);
  }
</script>

{#snippet toggleActions()}
  <SaveStatus state={status} />
  {#if ownsSwitch}
    <Switch
      checked={anyOn}
      label={t('modules.commandToggleAria', { trigger: command.trigger })}
      disabled={locked}
      onCheckedChange={(checked) => onToggleAll?.(triggers, checked)}
    />
  {/if}
{/snippet}

{#snippet triggerChip(trigger: string)}
  <Chip
    tone="inline"
    pressed={isOn(trigger)}
    disabled={locked}
    aria-label={t('modules.triggerToggleAria', { trigger: `!${trigger}` })}
    onclick={() => onToggleTrigger?.(trigger, !isOn(trigger))}
  >!{trigger}</Chip>
{/snippet}

{#snippet aliasLine()}
  <span class="also" role="group" aria-label={t('modules.triggersGroupAria', { trigger: command.trigger })}>
    {#if headToggle || staticAliases.length}
      <Text as="span" size="xs" tone="muted">{t('modules.aliasesLabel')}</Text>
    {/if}
    {#each toggles as trigger (trigger)}
      {@render triggerChip(trigger)}
    {/each}
    {#each staticAliases as alias (alias)}
      <Text as="span" size="xs" tone="muted-light" mono>{alias}</Text>
    {/each}
  </span>
{/snippet}

<div class="row-wrap">
  <ManagementRow
    selectable={!!onExpand}
    selected={expanded}
    {expanded}
    disabled={replyOff || !anyOn}
    onSelect={onExpand}
    actions={hasToggles ? toggleActions : undefined}
    below={toggles.length || staticAliases.length ? aliasLine : undefined}
  >
    {#snippet primary()}
      <span class="crow">
        <span class="trig">
          {#if headToggle}
            {@render triggerChip(triggers[0])}
          {:else}
            <Text as="span" size="sm" mono>{command.trigger}</Text>
          {/if}
        </span>
        <span class="sum"><Text as="span" size="sm" tone="muted-light">{tModuleCommandSummary(t, moduleId, command)}</Text></span>
        <Text as="span" size="xs" tone="muted">{permLabel}</Text>
      </span>
    {/snippet}
  </ManagementRow>
</div>

<style>
  .row-wrap {
    --row-below-indent: calc(var(--cmd-col, 112px) + 30px);
    display: block;
  }

  .crow {
    display: grid;
    grid-template-columns: var(--cmd-col, 112px) minmax(0, 1fr) auto;
    align-items: baseline;
    gap: 16px;
  }

  .trig {
    --chip-inline-size: var(--bb-text-sm);
    --chip-inline-color: var(--bb-white);
    --chip-inline-rule: transparent;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .sum { min-width: 0; }

  .also {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 4px 12px;
  }

  @media (max-width: 760px) {
    .row-wrap { --row-below-indent: 14px; }
    .crow { grid-template-columns: minmax(0, 1fr) auto; row-gap: 4px; }
    .sum { grid-column: 1 / -1; grid-row: 2; }
  }
</style>
