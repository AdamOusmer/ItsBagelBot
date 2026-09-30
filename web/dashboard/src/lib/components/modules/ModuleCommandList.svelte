<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
	import '@bagel/ui/styles/elements/layout.css';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type ModuleCommandInfo } from '@bagel/kit';
  import ModuleCommandRow from './ModuleCommandRow.svelte';

  const { t } = getI18n();

  let {
    commands,
    moduleId,
    headingId = 'module-cmds-h',
    sectionId = undefined as string | undefined
  }: {
    commands: readonly ModuleCommandInfo[];
    moduleId: string;
    headingId?: string;
    sectionId?: string;
  } = $props();
</script>

{#if commands.length}
  <div class="cmd-head" id={sectionId} tabindex="-1">
    <Heading level={6} as="h2" variant="eyebrow" id={headingId}>{t('modules.commandsTitle')}</Heading>
    <Text as="span" size="xs" tone="muted">{t('modules.commandsHint')}</Text>
  </div>
  <ul class="bb-list" aria-labelledby={headingId}>
    {#each commands as command, i (command.trigger)}
      <li><ModuleCommandRow {moduleId} {command} index={i + 1} /></li>
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
    border-bottom: 1px solid var(--bb-border);
    scroll-margin-top: calc(58px + env(safe-area-inset-top, 0px) + 56px);
  }
  .cmd-head:focus { outline: none; }
</style>
