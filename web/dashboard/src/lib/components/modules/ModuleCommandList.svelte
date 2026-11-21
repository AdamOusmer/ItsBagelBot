<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
	import '@bagel/ui/styles/elements/layout.css';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import {
    commandHeadTrigger,
    getI18n,
    moduleAllowedTriggers,
    moduleCommandTriggerMap,
    moduleDef,
    parseDisabledTriggers,
    toggleTriggerInList,
    toggleTriggersInList,
    type ModuleCommandInfo,
    type Perm
  } from '@bagel/kit';
  import ModuleCommandRow from './ModuleCommandRow.svelte';

  const { t } = getI18n();

  let {
    commands,
    moduleId,
    headingId = 'module-cmds-h',
    sectionId = undefined as string | undefined,
    expanded = null as string | null,
    roleOf = undefined,
    onExpand = undefined,
    disabledTriggers = '',
    locked = false,
    status = 'idle' as SaveState,
    onDisabledTriggersChange = undefined
  }: {
    commands: readonly ModuleCommandInfo[];
    moduleId: string;
    headingId?: string;
    sectionId?: string;
    expanded?: string | null;
    roleOf?: (command: ModuleCommandInfo) => Perm | undefined;
    onExpand?: (command: ModuleCommandInfo) => void;
    disabledTriggers?: string;
    locked?: boolean;
    status?: SaveState;
    onDisabledTriggersChange?: (next: string) => void;
  } = $props();

  let activeRow = $state<string | null>(null);

  const def = $derived(moduleDef(moduleId));
  const triggerMap = $derived(def ? moduleCommandTriggerMap(def) : new Map<string, string[]>());
  const allowed = $derived(def ? moduleAllowedTriggers(def) : new Set<string>());
  const disabledSet = $derived(parseDisabledTriggers(disabledTriggers));
  const interactive = $derived(!!onDisabledTriggersChange && allowed.size > 0);

  const headOff = $derived.by(() => {
    const off = new Map<string, boolean>();
    for (const owned of triggerMap.values()) {
      if (owned.length > 0) off.set(owned[0], owned.every((tr) => disabledSet.has(tr)));
    }
    return off;
  });

  function isParentOff(command: ModuleCommandInfo, owned: readonly string[]): boolean {
    if (owned.length > 0) return false;
    return headOff.get(commandHeadTrigger(command)) === true;
  }

  function toggleOne(rowTrigger: string, trigger: string, enabled: boolean) {
    if (!onDisabledTriggersChange) return;
    activeRow = rowTrigger;
    onDisabledTriggersChange(toggleTriggerInList(disabledTriggers, trigger, enabled, allowed));
  }

  function toggleAll(rowTrigger: string, triggers: readonly string[], enabled: boolean) {
    if (!onDisabledTriggersChange) return;
    activeRow = rowTrigger;
    onDisabledTriggersChange(toggleTriggersInList(disabledTriggers, triggers, enabled, allowed));
  }
</script>

{#if commands.length}
  <div class="cmd-head" id={sectionId} tabindex="-1">
    <Heading level={6} as="h2" variant="eyebrow" id={headingId}>{t('modules.commandsTitle')}</Heading>
    <Text as="span" size="xs" tone="muted">{t(interactive ? 'modules.commandsToggleHint' : 'modules.commandsHint')}</Text>
  </div>
  <Divider flush />
  <ul class="bb-list" aria-labelledby={headingId}>
    {#each commands as command, i (command.trigger)}
      {@const owned = triggerMap.get(command.trigger) ?? []}
      <li>
        <ModuleCommandRow
          {moduleId}
          {command}
          index={i + 1}
          role={roleOf?.(command)}
          expanded={expanded === command.trigger}
          onExpand={onExpand && command.preview ? () => onExpand(command) : undefined}
          triggers={interactive ? owned : []}
          disabledTriggers={disabledSet}
          parentOff={isParentOff(command, owned)}
          {locked}
          status={activeRow === command.trigger ? status : 'idle'}
          onToggleTrigger={interactive ? (tr, on) => toggleOne(command.trigger, tr, on) : undefined}
          onToggleAll={interactive ? (trs, on) => toggleAll(command.trigger, trs, on) : undefined}
        />
      </li>
    {/each}
  </ul>
{/if}

<style>
  .cmd-head {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    padding: 12px 18px;
    scroll-margin-top: calc(58px + env(safe-area-inset-top, 0px) + 56px);
  }
  .cmd-head:focus { outline: none; }
</style>
