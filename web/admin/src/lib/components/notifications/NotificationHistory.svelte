<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Pager from '@bagel/ui/svelte/Pager.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { NotificationWire } from '#lib/server/services';
  import { LEVEL_LABEL, LEVEL_TONE, audienceOf, type HistoryResult } from './notification-compose';

  let { history, rows, page, filtered, selectedId, controls, onSelect }: {
    history?: HistoryResult;
    rows: NotificationWire[];
    page: number;
    filtered: boolean;
    selectedId?: number;
    controls: string;
    onSelect: (notification: NotificationWire) => void;
  } = $props();

  const { t, locale } = getI18n();
  const numberFormat = new Intl.NumberFormat(locale);
  const dateTime = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });
  const currentPage = $derived(history?.page ?? page);

  function pageHref(value: number): string {
    return value > 1 ? `/notifications?page=${value}` : '/notifications';
  }

  function rowMeta(row: NotificationWire): string {
    const audience = audienceOf(row);
    return t('admin.notifications.rowMeta', {
      audience: t(audience.key, audience.params),
      who: row.created_by_login,
      when: dateTime.format(new Date(row.created_at))
    });
  }
</script>

<DeckList class="notification-history">
  <div aria-busy={!history}>
    {#if !history}
      <div aria-hidden="true">
        {#each Array(6) as _, i (i)}
          <div class="pending-row">
            <Skeleton variant="text" width="60%" height="1rem" />
            <Skeleton variant="text" width="42%" height="0.7rem" />
          </div>
        {/each}
      </div>
    {:else if history.degraded}
      <AlertBanner class="history-alert">
        {t('admin.notifications.degraded')}
      </AlertBanner>
    {:else if rows.length}
      <Stack class="history-list" as="ul" gap={0} role="list">
        {#each rows as row (row.id)}
          <ManagementRow
            as="li"
            id={`notification-row-${row.id}`}
            class="history-row"
            selected={selectedId === row.id}
            expanded={selectedId === row.id}
            controls={selectedId === row.id ? controls : undefined}
            onSelect={() => onSelect(row)}
            title={row.title}
            meta={rowMeta(row)}
          >
            {#snippet marks()}
              <span class="severity">
                <Tag contain tone={LEVEL_TONE[row.level]}><bb-fit>{t(LEVEL_LABEL[row.level])}</bb-fit></Tag>
              </span>
            {/snippet}
          </ManagementRow>
        {/each}
      </Stack>
    {:else}
      <EmptyState
        title={t(filtered ? 'admin.notifications.filterEmpty' : 'admin.notifications.empty')}
        body={t(filtered ? 'admin.notifications.filterEmptyBody' : 'admin.notifications.emptyBody')}
      />
    {/if}
  </div>

  <Pager
    label={t('admin.notifications.pagerLabel', { page: numberFormat.format(currentPage) })}
    prevHref={pageHref(currentPage - 1)}
    nextHref={pageHref(currentPage + 1)}
    hasPrev={!!history && currentPage > 1}
    hasNext={history?.hasMore ?? false}
    prevLabel={t('admin.notifications.pagerPrev')}
    nextLabel={t('admin.notifications.pagerNext')}
    fixed
    fit
  />
</DeckList>

<style>
  :global(.notification-history) {
    --notification-row-height: 3.75rem;
  }

  :global(.notification-history .history-list) {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  :global(.notification-history .history-row),
  .pending-row {
    block-size: var(--notification-row-height);
  }

  .pending-row {
    display: flex;
    justify-content: center;
    flex-direction: column;
    gap: var(--bb-space-1);
    padding-inline: var(--bb-space-4);
  }

  :global(.notification-history .history-alert) {
    margin: var(--bb-space-3);
  }

  .severity {
    text-align: end;
    inline-size: 6.5rem;
  }
</style>
