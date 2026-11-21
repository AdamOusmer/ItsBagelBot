<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { AuditEntry } from '#lib/server/services';

  export type StaffHistoryResult = { entries: AuditEntry[]; error: boolean };
</script>

<script lang="ts">
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  let { pending, onRetry }: { pending?: Promise<StaffHistoryResult>; onRetry: () => void } = $props();

  const { t, locale } = getI18n();
  const dateFormat = new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: 'UTC'
  });

  function retry() {
    document.getElementById('staff-history')?.focus();
    onRetry();
  }
</script>

{#snippet loading()}
  <div role="status" aria-label={t('admin.staff.historyLoading')}>
    <SkeletonStack rows={3} height="2rem" />
  </div>
{/snippet}

<Stack as="section" gap={2} aria-labelledby="staff-history-title">
  <Heading id="staff-history-title" level={3} variant="label">{t('admin.staff.historyTitle')}</Heading>
  <Scroller id="staff-history" class="staff-history-scroll" role="region" tabindex={0} aria-labelledby="staff-history-title">
    {#if pending}
      {#await pending}
        {@render loading()}
      {:then result}
        {#if result.error}
          <AlertBanner>
            {t('admin.staff.historyError')}
            {#snippet actions()}
              <Button variant="ghost" onclick={retry}>{t('common.retry')}</Button>
            {/snippet}
          </AlertBanner>
        {:else if result.entries.length}
          <ul class="history-list">
            {#each result.entries as entry (entry.id)}
              <li class="history-entry">
                <span class="dot" role="img" aria-label={t(entry.ok ? 'admin.audit.outcomeOk' : 'admin.audit.outcomeFailed')}>
                  <StatusDot tone={entry.ok ? 'success' : 'warning'} flat />
                </span>
                <div class="entry-content">
                  <Text size="xs" mono>{entry.action}{entry.target ? ` → ${entry.target}` : ''}</Text>
                  {#if entry.detail}<Text size="xs" mono tone="muted">{entry.detail}</Text>{/if}
                  {#if !entry.ok && entry.error}<Text size="xs" mono tone="danger">{entry.error}</Text>{/if}
                  <Text as="div" size="xs" tone="muted">
                    <time datetime={entry.created_at}>{dateFormat.format(new Date(entry.created_at))}</time>
                  </Text>
                </div>
              </li>
            {/each}
          </ul>
        {:else}
          <EmptyState title={t('admin.staff.historyEmpty')} />
        {/if}
      {/await}
    {:else}
      {@render loading()}
    {/if}
  </Scroller>
</Stack>

<style>
  :global(.staff-history-scroll) {
    block-size: 9rem;
    scrollbar-gutter: stable;
  }

  .history-list {
    display: grid;
    gap: var(--bb-space-3);
    padding: 0;
    margin: 0;
    list-style: none;
  }

  .history-entry {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--bb-space-2);
  }

  .dot {
    line-height: 1;
  }

  .entry-content {
    overflow-wrap: anywhere;
  }
</style>
