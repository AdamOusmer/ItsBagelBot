<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Code from '@bagel/ui/svelte/Code.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, tModuleCommandSummary, type ModuleCommandInfo } from '@bagel/kit';

  const { t } = getI18n();

  let { moduleId, command, index }: { moduleId: string; command: ModuleCommandInfo; index: number } = $props();

  const idx = $derived(String(index).padStart(2, '0'));
</script>

<ManagementRow selectable={false}>
  {#snippet primary()}
    <span class="crow">
      <span class="idx" aria-hidden="true"><Label mono as="span">{idx}</Label></span>
      <span class="cmd">
        <span><Code>{command.trigger}</Code></span>
        <Text as="span" size="xs" tone="muted" truncate>{tModuleCommandSummary(t, moduleId, command)}</Text>
      </span>
      {#if command.perm === 'mod'}
        <Tag tone="bare">{t('modules.permMods')}</Tag>
      {:else if command.perm === 'lead_mod'}
        <Tag tone="bare">{t('modules.permLeadMods')}</Tag>
      {:else}
        <span class="mini-spacer" aria-hidden="true"></span>
      {/if}
    </span>
  {/snippet}
</ManagementRow>

<style>
  .crow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { opacity: 0.55; }

  .cmd { display: flex; flex-direction: column; gap: 3px; min-width: 0; }

  .mini-spacer { width: 0; }

  @media (max-width: 760px) {
    .crow { grid-template-columns: minmax(0, 1fr) auto; }
    .idx { display: none; }
  }
</style>
