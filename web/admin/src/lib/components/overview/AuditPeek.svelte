<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/feed.css';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import { statusTone } from '@bagel/kit/status-tone';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AuditEntry } from '#lib/server/services';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { entries }: { entries: AuditEntry[] } = $props();

  const { t } = getI18n();

  function line(e: AuditEntry): string {
    return e.target ? `${e.action} → ${e.target}` : e.action;
  }
</script>

<Card as="section" class="overview-audit-panel">
  <CardHead title={t('admin.overview.auditTitle')} headingLevel={2}>
    {#snippet actions()}
      <a class="bb-card-head__more" href="/audit">{t('admin.overview.auditAll')}</a>
    {/snippet}
  </CardHead>

  {#if entries.length}
    <Scroller fill role="region" aria-label={t('admin.overview.auditTitle')} tabindex={0}>
      <div class="bb-node-list bb-stagger">
        {#each entries as e (e.id)}
          {@const action = line(e)}
          <div class="bb-node-list__row audit-row">
            <StatusDot tone={statusTone(e.ok ? 'online' : 'degraded')} />
            <span class="bb-node-list__name actor" title={e.actor_login}>@{e.actor_login}</span>
            <span class="bb-node-list__trail">{ago(e.created_at)}</span>
            <span class="detail">
              <Text as="span" size="xs" mono tone="muted" truncate title={action}>{action}</Text>
            </span>
            {#if !e.ok}
              {@const error = e.error || t('admin.overview.auditFailed')}
              <span class="detail">
                <Text as="span" size="xs" tone="danger" title={error}>{error}</Text>
              </span>
            {/if}
          </div>
        {/each}
      </div>
    </Scroller>
  {:else}
    <EmptyState title={t('admin.overview.auditEmpty')} />
  {/if}
</Card>

<style>
  :global(.overview-audit-panel) {
    display: flex;
    flex-direction: column;
    min-block-size: 0;
  }
  .audit-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: var(--bb-space-1) var(--bb-space-2);
    padding-block: var(--bb-space-2);
  }
  .actor {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .detail {
    grid-column: 2 / -1;
    overflow-wrap: anywhere;
  }
</style>
