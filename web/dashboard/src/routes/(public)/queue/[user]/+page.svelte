<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { page as route } from '$app/state';
  import { getI18n } from '@bagel/kit';
  import PublicHead from '#lib/components/public/PublicHead.svelte';
  import QueueBoardPage from '#lib/components/public/QueueBoardPage.svelte';
  import { liveBoard } from '#lib/live-board.svelte';
  import { MAX_QUEUE_ENTRIES, parseSnapshot, waitedSeconds } from '#lib/queue-snapshot';
  import { waitLabeler } from '#lib/wait-label';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const live = liveBoard(() => data.snapshot, parseSnapshot, () => route.url.pathname);
  onMount(live.start);

  const snapshot = $derived(live.snapshot);
  const trouble = $derived(live.trouble);

  const countFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const waitLabel = waitLabeler(locale, t('queuePage.justJoined'));

  const waits = $derived(snapshot.entries.map((entry) => waitedSeconds(entry, live.nowMs)));
  const longestWait = $derived(Math.max(1, ...waits));
  const upNext = $derived(snapshot.entries[0]);
  const shown = $derived(snapshot.entries.length);

  const rows = $derived(
    snapshot.entries.map((entry, i) => ({
      key: entry.login,
      position: i + 1,
      name: entry.login,
      face: entry.login,
      search: entry.login.toLowerCase(),
      wait: waitLabel(waits[i]),
      share: Math.max(0.04, waits[i] / longestWait)
    }))
  );

  const statusLine = $derived.by(() => {
    if (trouble) return t('queuePage.degraded');
    if (snapshot.total > shown) return t('queuePage.moreNote', { shown: countFmt.format(shown), total: countFmt.format(snapshot.total) });
    return '';
  });

  const empty = $derived(
    trouble
      ? { title: t('queuePage.degradedTitle'), body: t('queuePage.degraded') }
      : { title: t('queuePage.emptyTitle'), body: t('queuePage.emptyBody') }
  );

  const joinsOpen = $derived(snapshot.open && !snapshot.degraded);
  const statusText = $derived(
    t(snapshot.degraded ? 'queuePage.statusUnknown' : snapshot.open ? 'queuePage.statusOpen' : 'queuePage.statusClosed')
  );
</script>

<PublicHead
  title={t('queuePage.title', { channel: data.channelName })}
  description={t('queuePage.metaDescription', { channel: data.channelName })}
  url="https://queue.itsbagelbot.com/{data.login}"
/>

<QueueBoardPage
  eyebrow={t('queuePage.eyebrow')}
  channelName={data.channelName}
  tagline={t('queuePage.tagline')}
  hint={t('queuePage.joinHint')}
  next={{
    label: t('queuePage.upNext'),
    value: upNext?.login ?? t('queuePage.nobodyYet'),
    meta: upNext ? waitLabel(waits[0]) : t('queuePage.emptyTitle'),
    face: upNext?.login ?? null
  }}
  count={{ label: t('queuePage.inLine'), value: countFmt.format(snapshot.total), status: statusText, open: joinsOpen }}
  board={{
    title: t('queuePage.boardTitle'),
    note: t('queuePage.boardNote', { max: countFmt.format(MAX_QUEUE_ENTRIES) }),
    colName: t('queuePage.colViewer'),
    colWait: t('queuePage.colWaited'),
    searchLabel: t('queuePage.searchLabel'),
    searchPlaceholder: t('queuePage.searchPlaceholder')
  }}
  {rows}
  {trouble}
  {statusLine}
  {empty}
  noMatch={{ title: t('queuePage.noMatchTitle'), body: t('queuePage.noMatchBody') }}
/>
