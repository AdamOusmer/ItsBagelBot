<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { getI18n, type ModuleCommandInfo } from '@bagel/kit';
  import ModuleCommandRow from './ModuleCommandRow.svelte';

  const { t } = getI18n();

  let {
    commands,
    moduleId,
    headingId = 'module-cmds-h'
  }: {
    commands: readonly ModuleCommandInfo[];
    moduleId: string;
    headingId?: string;
  } = $props();
</script>

{#if commands.length}
  <div class="section-head cmd-head">
    <h2 id={headingId} class="section-title">{t('modules.commandsTitle')}</h2>
    <span class="cmd-head-hint">{t('modules.commandsHint')}</span>
  </div>
  <ul class="bb-list cmd-list" aria-labelledby={headingId}>
    {#each commands as command, i (command.trigger)}
      <li><ModuleCommandRow {moduleId} {command} index={i + 1} /></li>
    {/each}
  </ul>
{/if}

<style>
  .section-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 18px;
    border-bottom: 1px solid var(--rule);
  }
  .section-title {
    margin: 0;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan);
  }
  .cmd-head { flex-direction: column; align-items: flex-start; gap: 2px; }
  .cmd-head-hint { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .cmd-list > li:last-child :global(.row-shell) { border-bottom: none; }
</style>
