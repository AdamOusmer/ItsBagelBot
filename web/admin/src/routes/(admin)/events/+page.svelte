<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '@bagel/ui/styles/elements/feed.css';
  import { onMount } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { StatusTone } from '@bagel/kit/status-tone';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { FeedEvent } from '#lib/server/feed';

  type EventTone = FeedEvent['tone'];
  type Connection = 'connecting' | 'live' | 'reconnecting' | 'unavailable';

  const { locale, t } = getI18n();
  const dateFormat = new Intl.DateTimeFormat(locale, { dateStyle: 'long' });
  const timeFormat = new Intl.DateTimeFormat(locale, { timeStyle: 'medium' });
  const numberFormat = new Intl.NumberFormat(locale);
  const sourceOrder = new Intl.Collator(locale);

  const CAP = 500;
  const ALL_SOURCES = '*';
  const DENSITY_KEY = 'admin.events.density';
  const TONE_DOT: Record<EventTone, StatusTone> = {
    up: 'success',
    down: 'danger',
    neutral: 'neutral'
  };
  const STATUS = {
    connecting: { label: 'admin.events.connecting', tone: 'warning' },
    live: { label: 'admin.events.streaming', tone: 'success' },
    reconnecting: { label: 'admin.events.reconnecting', tone: 'warning' },
    unavailable: { label: 'admin.events.unavailable', tone: 'danger' },
    paused: { label: 'admin.events.paused', tone: 'neutral' }
  } as const;
  const TONE_LABEL = {
    all: 'admin.events.toneAll',
    up: 'admin.events.toneUp',
    down: 'admin.events.toneDown',
    neutral: 'admin.events.toneNeutral'
  } as const;
  const densityOptions = [
    { value: 'compact', label: t('admin.events.compact') },
    { value: 'detailed', label: t('admin.events.detailed') }
  ];

  let conn = $state<Connection>('connecting');
  let events = $state.raw<FeedEvent[]>([]);
  let pending = $state.raw<FeedEvent[]>([]);
  let hasHistory = $state(false);
  let liveAfter = $state(0);
  let paused = $state(false);
  let away = $state(false);
  let tone = $state('all');
  let source = $state(ALL_SOURCES);
  let search = $state('');
  let density = $state('detailed');

  let generationId = '';
  let clearedThrough = 0;
  let scroller: HTMLDivElement | undefined;

  const status = $derived(STATUS[paused && conn === 'live' ? 'paused' : conn]);
  const statusLabel = $derived(t(status.label));
  const unavailable = $derived(conn === 'unavailable');
  const queuedLabel = $derived(t('admin.events.pauseLabelBuffered', { n: numberFormat.format(pending.length) }));
  const resumeLabel = $derived(pending.length ? queuedLabel : t('admin.events.resumeLabel'));
  const pauseLabel = $derived(paused ? resumeLabel : t('admin.events.pauseLabel'));
  const showNew = $derived(away && !paused && pending.length > 0);
  const newLabel = $derived(t('admin.events.newEvents', { n: numberFormat.format(pending.length) }));
  const loaded = $derived(mergeEvents(events, pending));

  const toneOptions = $derived.by(() => {
    const counts = { all: loaded.length, up: 0, down: 0, neutral: 0 };
    for (const event of loaded) counts[event.tone]++;
    return (Object.keys(TONE_LABEL) as (keyof typeof TONE_LABEL)[]).map((value) => ({
      value,
      label: t(TONE_LABEL[value]),
      count: numberFormat.format(counts[value])
    }));
  });

  const sourceOptions = $derived.by(() => {
    const groups = new Set(loaded.map(sourceOf));
    if (source !== ALL_SOURCES) groups.add(source);
    return [
      { value: ALL_SOURCES, label: t('admin.events.sourceAll') },
      ...[...groups].sort(sourceOrder.compare).map((group) => ({ value: group, label: group }))
    ];
  });

  const rows = $derived.by(() => {
    const query = search.trim().toLocaleLowerCase(locale);
    let previousDay = '';
    let previousLive = false;
    return events.filter((event) => matches(event, query)).map((event) => {
      const at = new Date(event.at);
      const day = dateFormat.format(at);
      const live = event.id > liveAfter;
      const row = {
        event,
        day,
        time: timeFormat.format(at),
        dateDivider: previousDay !== day,
        liveDivider: previousLive && !live,
        live
      };
      previousDay = day;
      previousLive = live;
      return row;
    });
  });

  function sourceOf(event: FeedEvent): string {
    return event.label.split('.', 1)[0];
  }

  function matches(event: FeedEvent, query: string): boolean {
    const matchesTone = tone === 'all' || event.tone === tone;
    const matchesSource = source === ALL_SOURCES || sourceOf(event) === source;
    return matchesTone && matchesSource &&
      [event.label, event.subject, event.payload].some((text) => text.toLocaleLowerCase(locale).includes(query));
  }

  function mergeEvents(existing: FeedEvent[], incoming: FeedEvent[]): FeedEvent[] {
    if (!incoming.length) return existing;
    const byId = new Map(existing.map((event) => [event.id, event]));
    for (const event of incoming) byId.set(event.id, event);
    return [...byId.values()].sort((a, b) => b.id - a.id).slice(0, CAP);
  }

  function receive(incoming: FeedEvent[]) {
    const known = new Set([...events, ...pending].map((event) => event.id));
    const fresh = incoming.filter((event) => event.id > clearedThrough && !known.has(event.id));
    if (!fresh.length) return;
    if (paused || away) {
      pending = mergeEvents(pending, fresh);
      return;
    }
    events = mergeEvents(events, fresh);
  }

  function receiveHistory(incoming: FeedEvent[], generation: string) {
    conn = 'live';
    if (hasHistory && generationId === generation) {
      receive(incoming);
      return;
    }
    generationId = generation;
    clearedThrough = 0;
    liveAfter = incoming[0]?.id ?? 0;
    events = mergeEvents([], incoming);
    pending = [];
    hasHistory = true;
    scrollToLatest();
  }

  function parseMessage<T>(message: MessageEvent<string>): T | undefined {
    try {
      return JSON.parse(message.data) as T;
    } catch (error) {
      console.warn('Invalid event feed message', error);
    }
  }

  function receiveEvent(message: MessageEvent<string>) {
    const event = parseMessage<FeedEvent>(message);
    if (!event) return;
    conn = 'live';
    receive([event]);
  }

  onMount(() => {
    density = localStorage.getItem(DENSITY_KEY) === 'compact' ? 'compact' : 'detailed';
    const stream = new EventSource('/events/stream');
    stream.onerror = () => {
      if (conn !== 'unavailable') conn = 'reconnecting';
    };
    stream.addEventListener('history', (message: MessageEvent<string>) => {
      const history = parseMessage<FeedEvent[]>(message);
      if (history) receiveHistory(history, message.lastEventId);
    });
    stream.addEventListener('feed', receiveEvent);
    stream.addEventListener('unavailable', () => (conn = 'unavailable'));
    return () => stream.close();
  });

  function flushPending() {
    if (paused || away || !pending.length) return;
    events = mergeEvents(events, pending);
    pending = [];
  }

  function scrollToLatest() {
    if (scroller) scroller.scrollTop = 0;
    away = false;
    flushPending();
  }

  function jumpToLatest() {
    scroller?.focus({ preventScroll: true });
    scrollToLatest();
  }

  function trackScroll(event: Event & { currentTarget: HTMLDivElement }) {
    scroller = event.currentTarget;
    away = scroller.scrollTop > 1;
    flushPending();
  }

  function togglePause() {
    paused = !paused;
    flushPending();
  }

  function clearFeed() {
    clearedThrough = Math.max(clearedThrough, events[0]?.id ?? 0, pending[0]?.id ?? 0);
    events = [];
    pending = [];
    scrollToLatest();
  }

  function saveDensity(value: string) {
    localStorage.setItem(DENSITY_KEY, value);
  }
</script>

{#snippet liveDivider()}
  <li class="feed-divider feed-divider-live">
    <Label as="span">{t('admin.events.liveBegan')}</Label>
  </li>
{/snippet}

<section class="screen active">
  <PageHead description={t('admin.events.description')}>
    {t('admin.events.titlePre')}<em>{t('admin.events.titleEm')}</em>
  </PageHead>

  <PageToolbar style="display: block">
    {#snippet leading()}
      <div class="feed-filters">
        <div class="feed-tones" role="group" aria-label={t('admin.events.toneFilter')}>
          {#each toneOptions as option (option.value)}
            <Chip
              pressed={tone === option.value}
              style="justify-content: center; padding-inline: var(--bb-space-2); gap: var(--bb-space-1)"
              onclick={() => (tone = tone === option.value ? 'all' : option.value)}
            >
              <span class="tone-label"><bb-fit>{option.label}</bb-fit></span>
              <span class="tone-count">{option.count}</span>
            </Chip>
          {/each}
        </div>
        <Select fill options={sourceOptions} bind:value={source} label={t('admin.events.sourceFilter')} />
        <SearchInput
          fill
          bind:value={search}
          placeholder={t('admin.events.searchPlaceholder')}
          aria-label={t('admin.events.searchPlaceholder')}
        />
      </div>
    {/snippet}
  </PageToolbar>

  <Card>
    <div class="feed-head">
      <CardHead title={t('admin.events.subject')} headingLevel={2}>
        {#snippet actions()}
          <Tag bare status aria-describedby={unavailable ? 'feed-outage' : undefined}>
            <StatusDot tone={status.tone} />
            <span class="feed-status"><bb-fit>{statusLabel}</bb-fit></span>
          </Tag>
        {/snippet}
      </CardHead>
      <span id="feed-outage" class="feed-reason" class:shown={unavailable} aria-hidden={!unavailable}>
        <bb-fit>{t('admin.events.unavailableBody')}</bb-fit>
      </span>
    </div>

    <div class="feed-actions">
      <SegmentedControl
        even
        options={densityOptions}
        bind:value={density}
        onValueChange={saveDensity}
        label={t('admin.events.densityLabel')}
      />
      <div class="feed-actions-right">
        <span class="new-pill" class:shown={showNew} aria-hidden={!showNew}>
          <Chip style="border-radius: var(--bb-radius-pill)" disabled={!showNew} onclick={jumpToLatest}>
            <span class="new-label"><bb-fit>{newLabel}</bb-fit></span>
          </Chip>
        </span>
        <Button size="sm" variant="secondary" onclick={togglePause}>
          <span class="pause-label"><bb-fit>{pauseLabel}</bb-fit></span>
        </Button>
        <Button size="sm" variant="ghost" onclick={clearFeed} disabled={!loaded.length}>
          <span class="clear-label"><bb-fit>{t('admin.events.clear')}</bb-fit></span>
        </Button>
      </div>
    </div>

    <Scroller
      style="block-size: min(60vh, 34rem); scrollbar-gutter: stable"
      role="region"
      tabindex={0}
      aria-label={t('admin.events.listLabel')}
      aria-busy={!hasHistory && conn === 'connecting'}
      onscroll={trackScroll}
    >
      {#if !hasHistory && conn === 'connecting'}
        <SkeletonStack rows={8} height={density === 'compact' ? '2.25rem' : '3.5rem'} />
      {:else if !rows.length}
        <EmptyState
          title={events.length ? t('admin.events.emptyMatch') : statusLabel}
          body={events.length || unavailable ? undefined : t('admin.events.idleBody')}
        />
      {:else}
        <ul class="bb-feed" role="list">
          {#each rows as row (row.event.id)}
            {#if row.liveDivider}{@render liveDivider()}{/if}
            {#if row.dateDivider}
              <li class="feed-divider"><Label as="span">{row.day}</Label></li>
            {/if}
            <li class="bb-feed__row event-row" class:compact={density === 'compact'}>
              <span class="feed-dot" aria-label={t(TONE_LABEL[row.event.tone])} role="img">
                <StatusDot tone={TONE_DOT[row.event.tone]} />
              </span>
              <span class="bb-feed__body">
                <span class="bb-feed__title">{row.event.label}</span>
                {#if density === 'detailed' && row.event.payload}
                  <span class="feed-payload">
                    <Text as="span" size="xs" tone="muted" mono>{row.event.payload}</Text>
                  </span>
                {/if}
              </span>
              <time class="bb-feed__trail" datetime={row.event.at}>{row.time}</time>
            </li>
          {/each}
          {#if rows.at(-1)?.live}{@render liveDivider()}{/if}
        </ul>
      {/if}
    </Scroller>
  </Card>
</section>

<style>
  .feed-filters {
    display: grid;
    grid-template-columns: minmax(23rem, 1.5fr) minmax(8rem, 0.7fr) minmax(10rem, 1fr);
    align-items: center;
    gap: var(--bb-space-3);
  }

  .feed-tones {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--bb-space-1);
  }

  .tone-label {
    min-inline-size: 0;
  }

  .tone-count {
    flex: 0 0 3ch;
    text-align: end;
    font-size: var(--bb-text-xs);
  }

  .feed-status,
  .new-label,
  .pause-label,
  .clear-label {
    display: block;
    min-inline-size: 0;
    text-align: center;
  }

  .feed-head {
    display: flow-root;
    position: relative;
    margin-block-end: var(--bb-space-1);
    overflow-wrap: anywhere;
  }

  .feed-status {
    inline-size: 6rem;
  }

  .feed-reason {
    position: absolute;
    inset-block-end: 1px;
    inset-inline: 0;
    font-size: var(--bb-text-xs);
    line-height: 1;
    text-align: end;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--bb-dur-base) ease;
  }

  .feed-reason.shown {
    opacity: 1;
    pointer-events: auto;
  }

  .feed-actions {
    display: grid;
    grid-template-columns: minmax(9rem, 13rem) auto;
    justify-content: space-between;
    gap: var(--bb-space-2);
    margin-block-end: var(--bb-space-3);
  }

  .feed-actions-right {
    display: flex;
    align-items: center;
    justify-content: end;
    gap: var(--bb-space-2);
  }

  .new-pill {
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--bb-dur-base) ease;
  }

  .new-pill.shown {
    opacity: 1;
    pointer-events: auto;
  }

  .new-label {
    inline-size: 3.5rem;
  }

  .pause-label {
    inline-size: 7rem;
  }

  .clear-label {
    inline-size: 3.1rem;
  }

  .feed-divider {
    display: flex;
    align-items: center;
    gap: var(--bb-space-3);
    padding-block: var(--bb-space-2);
  }

  .feed-divider::after {
    content: '';
    flex: 1;
    border-block-start: 1px solid var(--bb-border);
  }

  .feed-divider-live::before {
    content: '';
    flex: 1;
    border-block-start: 1px solid var(--bb-border);
  }

  .event-row {
    overflow-wrap: anywhere;
  }

  .event-row.compact {
    padding-block: var(--bb-space-2);
  }

  .feed-dot {
    display: flex;
    padding-block-start: var(--bb-space-1);
  }

  .feed-payload {
    display: block;
    margin-block-start: 2px;
  }

  @media (max-width: 1100px) {
    .feed-filters {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .feed-tones {
      grid-column: 1 / -1;
    }
  }

  @media (max-width: 760px) {
    .feed-actions {
      grid-template-columns: 1fr;
    }

    .feed-actions-right {
      flex-wrap: wrap;
    }
  }

  @media (max-width: 460px) {
    .feed-filters {
      grid-template-columns: minmax(0, 1fr);
    }

    .feed-tones {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .feed-actions-right {
      justify-content: space-between;
      gap: var(--bb-space-1);
    }

    .new-label {
      inline-size: 3rem;
    }

    .pause-label {
      inline-size: 6rem;
    }

    .clear-label {
      inline-size: 2.6rem;
    }
  }

  @media (max-width: 375px) {
    .feed-status {
      inline-size: 4.5rem;
    }

    .pause-label {
      inline-size: 5rem;
    }

    .clear-label {
      inline-size: 2.4rem;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .feed-reason,
    .new-pill {
      transition: none;
    }
  }
</style>
