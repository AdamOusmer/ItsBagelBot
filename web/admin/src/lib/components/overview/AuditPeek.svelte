<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import { statusTone } from '@bagel/kit/status-tone';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AuditEntry } from '$lib/server/services';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { entries }: { entries: AuditEntry[] } = $props();

  const { t } = getI18n();

  function line(e: AuditEntry): string {
    return e.target ? `${e.action} → ${e.target}` : e.action;
  }
</script>

<Card as="section">
  <CardHead title={t('admin.overview.auditTitle')}>
    {#snippet action()}
      <a class="bb-card-head__more" href="/audit">{t('admin.overview.auditAll')}</a>
    {/snippet}
  </CardHead>

  {#if entries.length}
    <div class="bb-node-list bb-stagger">
      {#each entries as e (e.id)}
        <div class="bb-node-list__row">
          <StatusDot tone={statusTone(e.ok ? 'online' : 'degraded')} />
          <span class="bb-node-list__name">@{e.actor_login}</span>
          <span class="bb-node-list__meta"><Text as="span" size="xs" mono tone="muted" truncate>{line(e)}</Text></span>
          {#if !e.ok}
            <span class="err">
              <Tag tone="error">{e.error || t('admin.overview.auditFailed')}</Tag>
            </span>
          {/if}
          <span class="bb-node-list__trail">{ago(e.created_at)}</span>
        </div>
      {/each}
    </div>
  {:else}
    <EmptyState title={t('admin.overview.auditEmpty')} />
  {/if}
</Card>

<style>
  .err {
    max-width: 180px;
    min-width: 0;
    overflow: hidden;
  }
  @media (max-width: 760px) {
    .err {
      display: none;
    }
  }
</style>
