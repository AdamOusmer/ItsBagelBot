<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ActionBar from '@bagel/ui/svelte/ActionBar.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit';

  const { t } = getI18n();

  let {
    count,
    deletable,
    busy = false,
    onEnable,
    onDisable,
    onDelete,
    onAll,
    onDone
  }: {
    count: number;
    deletable: number;
    busy?: boolean;
    onEnable: () => void;
    onDisable: () => void;
    onDelete: () => void;
    onAll: () => void;
    onDone: () => void;
  } = $props();

  const none = $derived(count === 0 || busy);
</script>

<ActionBar role="toolbar" aria-label={t('commands.bulkBar')} style="margin-top: 12px">
  <span class="lead">
    <Text as="span" size="xs" mono role="status">{t('commands.selectedCount', { count })}</Text>
    <span class="hit"><Button variant="ghost" size="sm" onclick={onAll} disabled={busy}>{t('commands.selectAll')}</Button></span>
  </span>
  {#snippet actions()}
    <span class="hit"><Button variant="secondary" size="sm" onclick={onEnable} disabled={none}>{t('commands.bulkEnable')}</Button></span>
    <span class="hit"><Button variant="secondary" size="sm" onclick={onDisable} disabled={none}>{t('commands.bulkDisable')}</Button></span>
    <span class="hit"><Button size="sm" onclick={onDelete} disabled={none || deletable === 0} tone="danger">{t('commands.bulkDelete')}</Button></span>
    <span class="hit"><Button variant="ghost" size="sm" onclick={onDone} disabled={busy}>{t('commands.selectDone')}</Button></span>
  {/snippet}
</ActionBar>

<style>
  .lead { display: inline-flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .hit { display: inline-flex; }
  @media (pointer: coarse), (max-width: 760px) {
    .hit { min-height: 44px; }
  }
</style>
