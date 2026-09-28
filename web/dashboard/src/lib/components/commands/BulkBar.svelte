<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Button, getI18n } from '@bagel/kit';

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
  <span class="bulk-count" role="status">{t('commands.selectedCount', { count })}</span>
  <Button variant="ghost" size="sm" class="bulk-btn" onclick={onAll} disabled={busy}>{t('commands.selectAll')}</Button>
  <span class="bulk-grow"></span>
  <Button variant="secondary" size="sm" class="bulk-btn" onclick={onEnable} disabled={none}>{t('commands.bulkEnable')}</Button>
  <Button variant="secondary" size="sm" class="bulk-btn" onclick={onDisable} disabled={none}>{t('commands.bulkDisable')}</Button>
  <Button variant="destructive" size="sm" class="bulk-btn" onclick={onDelete} disabled={none || deletable === 0}>{t('commands.bulkDelete')}</Button>
  <Button variant="ghost" size="sm" class="bulk-btn" onclick={onDone} disabled={busy}>{t('commands.selectDone')}</Button>
</div>

<style>
  .bulk {
    position: sticky;
    bottom: 16px;
    z-index: 5;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
    padding: 10px 14px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-raised, var(--bb-bg));
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  }
  .bulk-count { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-white); }
  .bulk-grow { flex: 1; }
  @media (pointer: coarse), (max-width: 760px) {
    .bulk :global(.bulk-btn) { min-height: 44px; }
  }
</style>
