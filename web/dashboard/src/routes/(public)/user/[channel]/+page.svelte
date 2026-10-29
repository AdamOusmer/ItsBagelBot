<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import AuroraBg from '@bagel/ui/svelte/AuroraBg.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Code from '@bagel/ui/svelte/Code.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Glide from '@bagel/ui/svelte/Glide.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { commandsHref } from '@bagel/kit/site-links';
  import Mark from '@bagel/ui/svelte/Mark.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { mountGlide } from '@bagel/ui/lib/glide';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import { tick } from 'svelte';
  import { page } from '$app/state';
  import PublicHead from '#lib/components/public/PublicHead.svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();
  const { t } = getI18n();

  type Kind = 'custom' | 'module' | 'builtin';
  type Filter = 'all' | Kind;
  type Row = {
    key: string;
    trigger: string;
    aliases: string[];
    response: string;
    perm: string;
    cooldown: number;
    userCooldown: number;
    liveOnly: boolean;
    uses: string;
    kind: Kind;
    source: string;
    moduleId: string | null;
  };
  type Group = { key: string; title: string; tagline: string; rows: Row[] };

  const FILTERS: Array<{ id: Filter; label: string; heading: string }> = [
    { id: 'all', label: t('public.commands.filterAll'), heading: t('public.commands.filterAllHeading') },
    { id: 'custom', label: t('public.commands.filterCustom'), heading: t('public.commands.filterCustomHeading') },
    { id: 'module', label: t('public.commands.filterModule'), heading: t('public.commands.filterModuleHeading') },
    { id: 'builtin', label: t('public.commands.filterBuiltin'), heading: t('public.commands.filterBuiltinHeading') }
  ];

  let query = $state('');
  let filter = $state<Filter>('all');
  let activeGroup = $state<string | null>(null);

  const creatorCode = $derived(String(data.creatorCode ?? '').trim());

  const all = $derived.by((): Row[] => {
    const custom: Row[] = data.commands.map((c, i) => ({
      ...c,
      key: `custom:${i}:${c.trigger}`,
      kind: 'custom',
      source: t('public.commands.filterCustom'),
      moduleId: null
    }));
    const fromModules: Row[] = data.modules.flatMap((m) =>
      m.commands.map((c, i) => ({
        key: `${m.id}:${i}:${c.label}`,
        trigger: c.label,
        aliases: c.aliases,
        response: c.meta,
        perm: c.perm,
        cooldown: c.cooldown,
        userCooldown: 0,
        liveOnly: c.liveOnly,
        uses: '',
        kind: m.category === 'Built-in' ? 'builtin' : 'module',
        source: m.category === 'Built-in' ? t('public.commands.filterBuiltin') : m.label,
        moduleId: m.id
      }))
    );
    return [...custom, ...fromModules];
  });

  const q = $derived(query.trim().toLowerCase());

  const matchesQuery = (r: Row, needle: string) =>
    !needle ||
    [r.trigger, r.aliases.join(' '), r.response, r.source].some((field) => field.toLowerCase().includes(needle));

  const inScope = (r: Row) => filter === 'all' || r.kind === filter;

  const rows = $derived(all.filter((r) => inScope(r) && matchesQuery(r, q)));

  // Custom commands first, then one block per module (rows keep catalog order,
  // viewer commands before mod ones), then the built-ins together in one block.
  const groups = $derived.by((): Group[] => {
    const custom: Group = { key: 'custom', title: t('public.commands.filterCustomHeading'), tagline: '', rows: [] };
    const builtin: Group = { key: 'builtin', title: t('public.commands.filterBuiltinHeading'), tagline: '', rows: [] };
    const modules = new Map<string, Group>(
      data.modules
        .filter((m) => m.category !== 'Built-in')
        .toSorted((a, b) => a.label.localeCompare(b.label))
        .map((m) => [m.id, { key: m.id, title: m.label, tagline: m.tagline, rows: [] }])
    );
    for (const row of rows) {
      const group = row.kind === 'custom' ? custom : row.kind === 'builtin' ? builtin : modules.get(row.moduleId ?? '');
      group?.rows.push(row);
    }
    builtin.rows.sort((a, b) => a.trigger.localeCompare(b.trigger));
    return [custom, ...modules.values(), builtin].filter((g) => g.rows.length > 0);
  });

  const countOf = (id: Filter) => all.filter((r) => id === 'all' || r.kind === id).length;

  const filterOptions = $derived(FILTERS.map((f) => ({ value: f.id, label: f.label, count: countOf(f.id) })));

  const listHeading = $derived(`${FILTERS.find((f) => f.id === filter)?.heading} · ${rows.length}`);

  const groupKeyOf = (mod: { id: string; category: string }) => (mod.category === 'Built-in' ? 'builtin' : mod.id);

  // Same order as the blocks: modules alphabetically, built-ins last.
  const sideModules = $derived(
    data.modules.toSorted(
      (a, b) => Number(a.category === 'Built-in') - Number(b.category === 'Built-in') || a.label.localeCompare(b.label)
    )
  );

  let sideEl = $state<HTMLElement | null>(null);
  let modsEl = $state<HTMLElement | null>(null);
  let glide: ReturnType<typeof mountGlide> | null = null;

  $effect(() => {
    if (!modsEl) return;
    const handle = mountGlide(modsEl, { observe: modsEl, selector: '.mod.on' });
    glide = handle;
    return () => {
      handle();
      glide = null;
    };
  });

  // The glide follows the lit module as the reader scrolls.
  $effect(() => {
    void activeGroup;
    void sideModules;
    glide?.measure();
  });

  // Keep the lit module in view when the sidebar is its own scroller.
  $effect(() => {
    if (!sideEl || !activeGroup || sideEl.scrollHeight <= sideEl.clientHeight) return;
    const lit = sideEl.querySelector<HTMLElement>('.mod.on');
    if (!lit) return;
    const box = sideEl.getBoundingClientRect();
    const item = lit.getBoundingClientRect();
    if (item.top >= box.top && item.bottom <= box.bottom) return;
    sideEl.scrollTo({
      top: sideEl.scrollTop + item.top - box.top - box.height / 3,
      behavior: prefersReducedMotion() ? 'auto' : 'smooth'
    });
  });

  // Scroll-spy: the sidebar lights up the module block currently under the reading line.
  $effect(() => {
    const keys = groups.map((g) => g.key);
    const sections = keys.map((key) => document.getElementById(`group-${key}`)).filter((el) => el !== null);
    if (!sections.length || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) activeGroup = (entry.target as HTMLElement).dataset.group ?? null;
        }
      },
      { rootMargin: '-35% 0px -55% 0px' }
    );
    for (const section of sections) observer.observe(section);
    return () => observer.disconnect();
  });

  const summary = $derived(t('public.commands.summary', {
    commands: data.commands.length === 1
      ? t('public.commands.summaryOneCommand', { count: data.commands.length })
      : t('public.commands.summaryManyCommands', { count: data.commands.length }),
    modules: data.modules.length === 1
      ? t('public.commands.summaryOneModule', { count: data.modules.length })
      : t('public.commands.summaryManyModules', { count: data.modules.length }),
    total: all.length === 1
      ? t('public.commands.summaryOneThing', { count: 1 })
      : t('public.commands.summaryThings', { count: all.length })
  }));

  function pickFilter(id: string) {
    filter = id as Filter;
  }

  async function jumpTo(key: string) {
    if (!groups.some((g) => g.key === key)) {
      query = '';
      filter = 'all';
      await tick();
    }
    activeGroup = key;
    document.getElementById(`group-${key}`)?.scrollIntoView({ behavior: prefersReducedMotion() ? 'auto' : 'smooth', block: 'start' });
  }

  const COPY_FLASH_MS = 1400;
</script>

<PublicHead
  title={t('public.commands.pageTitle', { channel: data.channelName })}
  description={t('public.commands.pageDescription', { channel: data.channelName })}
  url={commandsHref(page.params.channel ?? '')}
/>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField /></div>
<div class="glow" aria-hidden="true"></div>

<main class="page">
  <header class="hero">
    <div class="hero__text">
      <span class="eyebrow"><Eyebrow tone="go">{t('public.commands.eyebrow')}</Eyebrow></span>
      <Heading level={1}>{data.channelName}</Heading>
      <Text tone="muted">{summary}</Text>
    </div>
    {#if creatorCode}
      <CopySurface
        text={creatorCode}
        label={t('public.commands.copyCreator')}
        hint={t('public.commands.clickToCopy')}
        copiedLabel={t('common.copied')}
        flashMs={COPY_FLASH_MS}
        title={t('public.commands.copyCreator')}
        announce={t('public.commands.copiedAnnounce', { trigger: creatorCode })}
      />
    {/if}
  </header>

  {#if data.degraded}
    <div class="notice">
      <AlertBanner tone="warning">{t('public.commands.dataUnavailable')}</AlertBanner>
    </div>
  {/if}

  <div class="toolbar">
    <div class="search">
      <SearchInput bind:value={query} placeholder={t('public.commands.searchPlaceholder')} />
    </div>
    <div class="filters">
      <SegmentedControl
        options={filterOptions}
        value={filter}
        label={t('public.commands.sourceLabel')}
        onValueChange={pickFilter}
      />
    </div>
  </div>

  <div class="columns">
    <section class="list-wrap" aria-label={t('public.commands.commandsLabel')}>
      <div class="list__head">
        <Label mono as="span">{listHeading}</Label>
        <Label mono as="span">{t('public.commands.clickRow')}</Label>
      </div>

      {#if rows.length}
        {#each groups as group, i (group.key)}
          <section
            class="group reveal"
            id={`group-${group.key}`}
            data-group={group.key}
            style:--i={i}
            aria-labelledby={`group-${group.key}-title`}
          >
            <Card atmo flush>
              <header class="group__head">
                <span class="group__title">
                  <Mark class="group__mark" />
                  <Heading level={2} variant="card" id={`group-${group.key}-title`}>{group.title}</Heading>
                  <span class="group__count"><Label mono as="span">{group.rows.length}</Label></span>
                </span>
                {#if group.tagline}
                  <Text size="sm" tone="muted">{group.tagline}</Text>
                {/if}
              </header>
              <ul class="rows">
                {#each group.rows as row (row.key)}
                  <li>
                    <CopySurface
                      variant="row"
                      text={row.trigger}
                      hint=""
                      copiedLabel={t('common.copied')}
                      flashMs={COPY_FLASH_MS}
                      announce={t('public.commands.copiedAnnounce', { trigger: row.trigger })}
                    >
                      <span class="row__trigger">
                        <Code tone="success">{row.trigger}</Code>
                        {#if row.aliases.length}
                          <Text as="span" size="xs" tone="muted" mono>{row.aliases.join(' ')}</Text>
                        {/if}
                      </span>
                      <span class="row__response"><Text as="span">{row.response}</Text></span>
                      <span class="row__tags">
                        {#if row.perm}
                          <Tag tone="bare">{row.perm}</Tag>
                        {/if}
                        {#if row.cooldown > 0}
                          <Tag tone="bare" title={t('public.commands.cooldownSeconds', { n: row.cooldown })}>
                            <Icon name="clock" size={11} />
                            <span aria-hidden="true">{row.cooldown}s</span>
                            <VisuallyHidden>{t('public.commands.cooldownSeconds', { n: row.cooldown })}</VisuallyHidden>
                          </Tag>
                        {/if}
                        {#if row.userCooldown > 0}
                          <Tag tone="bare" title={t('public.commands.userCooldownSeconds', { n: row.userCooldown })}>
                            <Icon name="clock" size={11} />
                            <span aria-hidden="true">{t('public.commands.userCooldownShort', { n: row.userCooldown })}</span>
                            <VisuallyHidden>{t('public.commands.userCooldownSeconds', { n: row.userCooldown })}</VisuallyHidden>
                          </Tag>
                        {/if}
                        {#if row.liveOnly}
                          <Tag tone="live" mark="solid" sweep>{t('public.commands.liveOnly')}</Tag>
                        {/if}
                        {#if row.uses}
                          <Label mono as="span">{t('public.commands.uses', { count: row.uses })}</Label>
                        {/if}
                      </span>
                    </CopySurface>
                  </li>
                {/each}
              </ul>
            </Card>
          </section>
        {/each}
      {:else}
        <Card atmo>
          {#if all.length === 0}
            <EmptyState title={t('public.commands.noCommands')} />
          {:else}
            <EmptyState title={t('public.commands.noMatches', { query })} />
          {/if}
        </Card>
      {/if}
    </section>

    <aside class="side" bind:this={sideEl}>
      <div class="modules">
        <Card atmo>
          <div class="side__head">
            <Label mono as="span">{t('public.commands.activeModules')}</Label>
            <Label mono as="span">{data.modules.length}</Label>
          </div>
          {#if data.modules.length}
            <div class="mods-wrap" bind:this={modsEl}>
              <Glide class="mods__glide" />
              <ul class="mods">
                {#each sideModules as mod (mod.id)}
                  {@const n = mod.commands.length}
                  {@const key = groupKeyOf(mod)}
                  <li>
                    <button
                      class="mod"
                      class:on={n > 0 && activeGroup === key}
                      type="button"
                      disabled={n === 0}
                      aria-current={n > 0 && activeGroup === key ? 'true' : undefined}
                      onclick={() => jumpTo(key)}
                    >
                      <Mark class="mod__mark" />
                      <span class="mod__text">
                        <span class="mod__label">{mod.label}</span>
                        <span class="mod__tagline">{mod.tagline}</span>
                      </span>
                      <span class="mod__meta">{n ? (n === 1 ? t('public.commands.moduleCount', { count: n }) : t('public.commands.moduleCountMany', { count: n })) : t('public.commands.moduleAuto')}</span>
                    </button>
                  </li>
                {/each}
              </ul>
            </div>
          {:else}
            <Text size="sm" tone="muted">{t('public.commands.noModules')}</Text>
          {/if}
        </Card>
      </div>

      <div class="legend">
        <Card>
          <div class="legend__body">
            <Label mono as="span">{t('public.commands.legendTitle')}</Label>
            <Text size="sm" tone="muted">
              {t('public.commands.legend')}
            </Text>
          </div>
        </Card>
      </div>
    </aside>
  </div>
</main>

<style>
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  .glow {
    position: absolute;
    left: 50%;
    top: -160px;
    width: min(1100px, 100vw);
    height: 640px;
    transform: translateX(-50%);
    pointer-events: none;
    background: radial-gradient(
      50% 55% at 50% 45%,
      rgba(var(--bb-tan-rgb), 0.18) 0%,
      rgba(var(--bb-green-glow-rgb), 0.08) 40%,
      transparent 72%
    );
    filter: blur(18px);
  }

  .page {
    --label-mono-size: var(--bb-text-xs);
    --copy-label-size: var(--bb-text-xs);

    position: relative;
    z-index: 1;
    max-width: 1080px;
    margin: 0 auto;
    padding: calc(var(--bb-nav-height) + env(safe-area-inset-top, 0px) + 72px) 24px 96px;
    color: var(--bb-white);
  }

  .hero {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: 24px 40px;
    margin-bottom: 44px;
  }

  .hero__text {
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .eyebrow {
    text-shadow: 0 0 16px rgba(var(--bb-green-glow-rgb), 0.45);
  }

  .notice { margin-bottom: 24px; }

  .toolbar {
    position: sticky;
    top: calc(var(--bb-nav-height) + env(safe-area-inset-top, 0px));
    z-index: var(--bb-z-sticky);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding: 12px 0 22px;
    background: linear-gradient(180deg, var(--bb-black) 78%, transparent);
  }

  .search { flex: 1 1 260px; min-width: 0; }

  .columns {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 28px;
    margin-top: 10px;
  }

  .list-wrap { flex: 999 1 520px; min-width: 0; }
  .side {
    --card-pad: 22px;

    flex: 1 1 260px;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .list__head, .side__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .list__head { padding: 0 4px 14px; }
  .side__head { margin-bottom: 16px; }

  .rows, .mods { list-style: none; margin: 0; padding: 0; }

  .group {
    --bb-reveal-distance: 18px;
    --bb-reveal-step: 60ms;

    scroll-margin-top: calc(var(--bb-nav-height) + env(safe-area-inset-top, 0px) + 96px);
  }
  .group + .group { margin-top: 18px; }
  .group__head {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 22px 24px 14px;
    border-bottom: 1px solid rgba(var(--bb-tan-rgb), 0.08);
    background: linear-gradient(180deg, rgba(var(--bb-green-glow-rgb), 0.06), transparent);
  }
  .group__head::after {
    content: '';
    position: absolute;
    left: 24px;
    bottom: -1px;
    width: 56px;
    height: 1px;
    background: linear-gradient(90deg, var(--bb-green-glow), transparent);
    box-shadow: 0 0 12px rgba(var(--bb-green-glow-rgb), 0.6);
  }
  .group__title {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .group__title :global(.group__mark) {
    flex: none;
    color: var(--bb-green-glow);
    filter: drop-shadow(0 0 8px rgba(var(--bb-green-glow-rgb), 0.55));
  }
  .group__count {
    margin-left: auto;
    padding: 2px 9px;
    border: 1px solid rgba(var(--bb-tan-rgb), 0.16);
    border-radius: 999px;
    color: var(--bb-tan);
  }

  .row__trigger {
    flex: 0 0 168px;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
    overflow-wrap: anywhere;
  }
  .row__response {
    display: block;
    flex: 1 1 240px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .row__tags {
    flex: 0 0 196px;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
    padding-top: 2px;
  }

  .mods-wrap { position: relative; }
  .mods { display: flex; flex-direction: column; }

  @media (min-width: 900px) {
    .side {
      position: sticky;
      top: calc(var(--bb-nav-height) + env(safe-area-inset-top, 0px) + 84px);
      max-height: calc(100dvh - var(--bb-nav-height) - env(safe-area-inset-top, 0px) - 108px);
      overflow-y: auto;
      overscroll-behavior: contain;
      scrollbar-width: none;
    }
    .side::-webkit-scrollbar { display: none; }
  }
  .mod {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    width: calc(100% + 16px);
    margin: 0 -8px;
    padding: 11px 8px;
    border: 0;
    border-radius: var(--bb-radius-sm);
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    transition: background var(--bb-dur-fast);
  }
  .mod:disabled { cursor: default; }
  .mod:not(:disabled, .on):hover, .mod:not(.on):focus-visible { background: rgba(var(--bb-tan-rgb), 0.06); }
  .mod.on .mod__label { color: var(--bb-white); }
  .mod :global(.mod__mark) { color: var(--bb-green-glow); }
  .mod__text { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .mod__label { font-family: var(--bb-font-body); font-size: 14px; font-weight: 600; color: var(--bb-white); }
  .mod__tagline {
    font-family: var(--bb-font-body);
    font-size: var(--bb-text-xs);
    line-height: 1.4;
    color: var(--bb-muted);
    text-wrap: pretty;
  }
  .mod__meta { font-family: var(--bb-font-mono); font-size: var(--bb-text-xs); color: var(--bb-tan); white-space: nowrap; }

  .legend__body {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  @media (max-width: 640px) {
    .page { padding-left: 16px; padding-right: 16px; }
    .toolbar { display: contents; }
    .search {
      position: sticky;
      top: calc(var(--bb-nav-height) + env(safe-area-inset-top, 0px));
      z-index: var(--bb-z-sticky);
      padding: 12px 0;
      background: var(--bb-black);
    }
    .filters { margin-bottom: 16px; }
    .columns { flex-direction: column; flex-wrap: nowrap; align-items: stretch; gap: 14px; margin-top: 0; }
    .list-wrap { flex: 0 0 auto; order: 3; }
    .side { display: contents; }
    .modules { --card-pad: 14px; order: 2; }
    .legend { --card-pad: 14px 16px; order: 4; }
    .mods { flex-direction: row; gap: 8px; overflow-x: auto; scrollbar-width: none; }
    .mods::-webkit-scrollbar { display: none; }
    .mods li { flex: 0 0 auto; }
    .mod { width: auto; margin: 0; min-height: 44px; padding: 8px 12px; border: 1px solid var(--bb-border); }
    .mod.on { border-color: rgba(var(--bb-green-glow-rgb), 0.45); }
    .mods-wrap :global(.mods__glide) { display: none; }
    .mod__tagline { display: none; }
    .side__head { margin-bottom: 10px; }
    .row__trigger { flex-basis: 100%; }
    .row__tags { flex-basis: 100%; justify-content: flex-start; }
    .list__head { padding: 14px 18px; }
    .group__head { padding: 16px 18px 12px; }
    .group__head::after { left: 18px; }
  }
</style>
