<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The chat-commands reference every module page shares: a tan caption plus
  // the read-only ModuleCommandRow ledger. Bespoke pages (quotes, songqueue,
  // loyalty) and the generic /modules/[id] inspector both render this inside a
  // DeckList so the list is 1:1 across surfaces — a page-local copy drifted
  // quotes onto a 16px white heading while [id] kept the 12px tan caption.
  import { getI18n, type ModuleCommandInfo } from '@bagel/shared';
  import ModuleCommandRow from './ModuleCommandRow.svelte';

  const { t } = getI18n();

  let {
    commands,
    headingId = 'module-cmds-h'
  }: {
    commands: readonly ModuleCommandInfo[];
    headingId?: string;
  } = $props();
</script>

{#if commands.length}
  <div class="section-head cmd-head">
    <h2 id={headingId} class="section-title">{t('modules.commandsTitle')}</h2>
    <span class="cmd-head-hint">{t('modules.commandsHint')}</span>
  </div>
  <ul class="list" aria-labelledby={headingId}>
    {#each commands as command, i (command.trigger)}
      <li><ModuleCommandRow {command} index={i + 1} /></li>
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

  .list { list-style: none; margin: 0; padding: 0; }
  .list > li:last-child :global(.row-shell) { border-bottom: none; }
</style>
