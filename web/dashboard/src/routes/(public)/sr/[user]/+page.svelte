<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { page as route } from '$app/state';
  import { getI18n } from '@bagel/kit';
  import PublicHead from '#lib/components/public/PublicHead.svelte';
  import QueueBoardPage from '#lib/components/public/QueueBoardPage.svelte';
  import { liveBoard } from '#lib/live-board.svelte';
  import { waitedSeconds } from '#lib/queue-snapshot';
  import { MAX_SONG_ROWS, parseSongSnapshot, type SongRow } from '#lib/songqueue-snapshot';
  import { waitLabeler } from '#lib/wait-label';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const live = liveBoard(() => data.snapshot, parseSongSnapshot, () => route.url.pathname);
  onMount(live.start);

  const snapshot = $derived(live.snapshot);
  const trouble = $derived(live.trouble);

  const countFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const waitLabel = waitLabeler({ locale, justJoined: t('songPage.justJoined') });

  const credit = (song: SongRow) => [song.artists, song.requester].filter(Boolean).join(' · ');
  const faceOf = (song: SongRow) => song.requester || data.login;

  const waits = $derived(snapshot.up.map((song) => waitedSeconds(song, live.nowMs)));
  const longestWait = $derived(Math.max(1, ...waits));
  const shown = $derived(snapshot.up.length);

  const rows = $derived(
    snapshot.up.map((song, i) => ({
      key: String(i),
      position: i + 1,
      name: song.title,
      detail: credit(song),
      face: faceOf(song),
      search: `${song.title} ${song.artists} ${song.requester}`.toLowerCase(),
      wait: waitLabel(waits[i]),
      share: Math.max(0.04, waits[i] / longestWait)
    }))
  );

  const statusLine = $derived.by(() => {
    if (trouble) return t('songPage.degraded');
    if (snapshot.total > shown) return t('songPage.moreNote', { shown: countFmt.format(shown), total: countFmt.format(snapshot.total) });
    return '';
  });

  const empty = $derived(
    trouble
      ? { title: t('songPage.degradedTitle'), body: t('songPage.degraded') }
      : { title: t('songPage.emptyTitle'), body: t('songPage.emptyBody') }
  );

  const requestsOpen = $derived(snapshot.open && !snapshot.degraded);
  const statusText = $derived(
    t(snapshot.degraded ? 'songPage.statusUnknown' : snapshot.open ? 'songPage.statusOpen' : 'songPage.statusClosed')
  );
  const current = $derived(snapshot.current);
</script>

<PublicHead
  title={t('songPage.title', { channel: data.channelName })}
  description={t('songPage.metaDescription', { channel: data.channelName })}
  url="https://queue.itsbagelbot.com/sr/{data.login}"
/>

<QueueBoardPage
  eyebrow={t('songPage.eyebrow')}
  channelName={data.channelName}
  tagline={t('songPage.tagline')}
  hint={t('songPage.requestHint')}
  next={{
    label: t('songPage.nowPlaying'),
    value: current?.title ?? t('songPage.nothingPlaying'),
    meta: current ? credit(current) : t('songPage.emptyTitle'),
    face: current ? faceOf(current) : null
  }}
  count={{ label: t('songPage.inQueue'), value: countFmt.format(snapshot.total), status: statusText, open: requestsOpen }}
  board={{
    title: t('songPage.boardTitle'),
    note: t('songPage.boardNote', { max: countFmt.format(MAX_SONG_ROWS) }),
    colName: t('songPage.colSong'),
    colWait: t('songPage.colWaited'),
    searchLabel: t('songPage.searchLabel'),
    searchPlaceholder: t('songPage.searchPlaceholder')
  }}
  {rows}
  {trouble}
  {statusLine}
  {empty}
  noMatch={{ title: t('songPage.noMatchTitle'), body: t('songPage.noMatchBody') }}
/>
