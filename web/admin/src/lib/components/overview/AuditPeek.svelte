<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The last handful of operator actions. Managers only -- the caller gates the
  // whole panel on allows(role, 'audit.read'), matching the /audit route.
  import Card from '@bagel/shared/components/Card.svelte';
  import CardHead from '@bagel/shared/components/CardHead.svelte';
  import EmptyState from '@bagel/shared/components/EmptyState.svelte';
  import { statusTone } from '@bagel/shared/status-tone';
  import { ago } from '@bagel/shared';
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { AuditEntry } from '$lib/server/services';
  import StatusDot from '../StatusDot.svelte';

  let { entries }: { entries: AuditEntry[] } = $props();

  const { t } = getI18n();

  function line(e: AuditEntry): string {
    return e.target ? `${e.action} → ${e.target}` : e.action;
  }
</script>

<Card as="section">
  <CardHead title={t('admin.overview.auditTitle')}>
    {#snippet action()}
      <a class="more" href="/audit">{t('admin.overview.auditAll')}</a>
    {/snippet}
  </CardHead>

  {#if entries.length}
    <div class="node-list">
      {#each entries as e (e.id)}
        <div class="node-row">
          <StatusDot tone={statusTone(e.ok ? 'online' : 'degraded')} />
          <span class="nm">@{e.actor_login}</span>
          <span class="sv mono">{line(e)}</span>
          {#if !e.ok}
            <span class="err">{e.error || t('admin.overview.auditFailed')}</span>
          {/if}
          <span class="pg">{ago(e.created_at)}</span>
        </div>
      {/each}
    </div>
  {:else}
    <EmptyState title={t('admin.overview.auditEmpty')} />
  {/if}
</Card>

<style>
  .node-row .sv.mono {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .err {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-status-error);
    background: var(--bb-status-error-bg);
    border: 1px solid var(--bb-status-error-border);
    border-radius: var(--bb-radius-pill);
    padding: 2px 8px;
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (max-width: 760px) {
    .err {
      display: none;
    }
  }
</style>
