<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Button, Text } from '@bagel/ui/svelte';
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

<div class="bulk" role="toolbar" aria-label={t('commands.bulkBar')}>
  <Text as="span" size="xs" mono role="status">{t('commands.selectedCount', { count })}</Text>
  <span class="hit"><Button variant="ghost" size="sm" onclick={onAll} disabled={busy}>{t('commands.selectAll')}</Button></span>
  <span class="bulk-grow"></span>
  <span class="hit"><Button variant="secondary" size="sm" onclick={onEnable} disabled={none}>{t('commands.bulkEnable')}</Button></span>
  <span class="hit"><Button variant="secondary" size="sm" onclick={onDisable} disabled={none}>{t('commands.bulkDisable')}</Button></span>
  <span class="hit"><Button size="sm" onclick={onDelete} disabled={none || deletable === 0} tone="danger">{t('commands.bulkDelete')}</Button></span>
  <span class="hit"><Button variant="ghost" size="sm" onclick={onDone} disabled={busy}>{t('commands.selectDone')}</Button></span>
</div>

<style>
  .bulk {
    position: sticky;
    bottom: 16px;
    z-index: var(--bb-z-sticky);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
    padding: 10px 14px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    background: var(--bb-card-bg);
    box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.35);
  }
  .bulk-grow { flex: 1; }
  .hit { display: inline-flex; }
  @media (pointer: coarse), (max-width: 760px) {
    .hit { min-height: 44px; }
  }
</style>
