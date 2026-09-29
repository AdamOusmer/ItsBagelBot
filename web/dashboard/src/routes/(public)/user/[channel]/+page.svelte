<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner, Card, Code, CopySurface, EmptyState, Eyebrow, Heading, Icon, Label, LightField, SearchInput, SegmentedControl, Tag, Text } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { commandsHref } from '@bagel/kit/site-links';
  import Mark from '@bagel/ui/svelte/Mark.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { page } from '$app/state';
  import PublicHead from '$lib/components/public/PublicHead.svelte';
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
    liveOnly: boolean;
    uses: string;
    kind: Kind;
    source: string;
    moduleId: string | null;
  };

  const FILTERS: Array<{ id: Filter; label: string; heading: string }> = [
    { id: 'all', label: t('public.commands.filterAll'), heading: t('public.commands.filterAllHeading') },
    { id: 'custom', label: t('public.commands.filterCustom'), heading: t('public.commands.filterCustomHeading') },
    { id: 'module', label: t('public.commands.filterModule'), heading: t('public.commands.filterModuleHeading') },
    { id: 'builtin', label: t('public.commands.filterBuiltin'), heading: t('public.commands.filterBuiltinHeading') }
  ];

  let query = $state('');
  let filter = $state<Filter>('all');
  let moduleId = $state<string | null>(null);

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
        aliases: [],
        response: c.meta,
        perm: '',
        cooldown: 0,
        liveOnly: false,
        uses: '',
        kind: m.category === 'Built-in' ? 'builtin' : 'module',
        source: m.category === 'Built-in' ? t('public.commands.filterBuiltin') : m.label,
        moduleId: m.id
      }))
    );
    return [...custom, ...fromModules];
  });

  const q = $derived(query.trim().toLowerCase());

  const rows = $derived(
    all
      .filter((r) => (moduleId ? r.moduleId === moduleId : filter === 'all' || r.kind === filter))
      .filter(
        (r) =>
          !q ||
          r.trigger.toLowerCase().includes(q) ||
          r.aliases.join(' ').toLowerCase().includes(q) ||
          r.response.toLowerCase().includes(q) ||
          r.source.toLowerCase().includes(q)
      )
      .sort((a, b) => a.trigger.localeCompare(b.trigger))
  );

  const countOf = (id: Filter) => all.filter((r) => id === 'all' || r.kind === id).length;

  const filterOptions = $derived(FILTERS.map((f) => ({ value: f.id, label: f.label, count: countOf(f.id) })));

  const activeModule = $derived(data.modules.find((m) => m.id === moduleId) ?? null);
  const listHeading = $derived(
    `${activeModule ? activeModule.label : FILTERS.find((f) => f.id === filter)?.heading} · ${rows.length}`
  );

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
    moduleId = null;
  }

  function pickModule(id: string) {
    moduleId = moduleId === id ? null : id;
    filter = 'all';
  }

  const COPY_FLASH_MS = 1400;
</script>

<PublicHead
  title={t('public.commands.pageTitle', { channel: data.channelName })}
  description={t('public.commands.pageDescription', { channel: data.channelName })}
  url={commandsHref(page.params.channel ?? '')}
/>

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
      <AlertBanner variant="warn">{t('public.commands.dataUnavailable')}</AlertBanner>
    </div>
  {/if}

  <div class="toolbar">
    <div class="search">
      <SearchInput bind:value={query} placeholder={t('public.commands.searchPlaceholder')} />
    </div>
    <div class="filters">
      <SegmentedControl
        options={filterOptions}
        value={moduleId ? '' : filter}
        label={t('public.commands.sourceLabel')}
        onchange={pickFilter}
      />
    </div>
  </div>

  <div class="columns">
    <section class="list-wrap" aria-label={t('public.commands.commandsLabel')}>
      <Card atmo flush>
        <div class="list__head">
          <Label mono as="span">{listHeading}</Label>
          <Label mono as="span">{t('public.commands.clickRow')}</Label>
        </div>

        {#if rows.length}
          <ul class="rows">
            {#each rows as row (row.key)}
              <li>
                <CopySurface
                  variant="row"
                  text={row.trigger}
                  copiedLabel={t('common.copied')}
                  flashMs={COPY_FLASH_MS}
                  announce={t('public.commands.copiedAnnounce', { trigger: row.trigger })}
                >
                  <span class="row__trigger">
                    <Code tone="positive">{row.trigger}</Code>
                    {#if row.aliases.length}
                      <Text as="span" size="xs" tone="muted" mono>{row.aliases.join(' ')}</Text>
                    {/if}
                  </span>
                  <span class="row__response"><Text as="span">{row.response}</Text></span>
                  <span class="row__tags">
                    <Tag tone="alpha">{row.source}</Tag>
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
        {:else if all.length === 0}
          <EmptyState title={t('public.commands.noCommands')} />
        {:else}
          <EmptyState title={t('public.commands.noMatches', { query })} />
        {/if}
      </Card>
    </section>

    <aside class="side">
      <div class="modules">
        <Card atmo>
          <div class="side__head">
            <Label mono as="span">{t('public.commands.activeModules')}</Label>
            <Label mono as="span">{data.modules.length}</Label>
          </div>
          {#if data.modules.length}
            <ul class="mods">
              {#each data.modules as mod (mod.id)}
                {@const n = mod.commands.length}
                <li>
                  <button
                    class="mod"
                    class:on={moduleId === mod.id}
                    type="button"
                    disabled={n === 0}
                    aria-pressed={moduleId === mod.id}
                    onclick={() => pickModule(mod.id)}
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
    width: min(1100px, 140vw);
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
  .list__head { padding: 16px 24px; border-bottom: 1px solid var(--bb-border); }
  .side__head { margin-bottom: 16px; }

  .rows, .mods { list-style: none; margin: 0; padding: 0; }

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
    flex: 0 0 auto;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
    padding-top: 2px;
  }

  .mods { display: flex; flex-direction: column; }
  .mod {
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
  .mod:not(:disabled):hover, .mod:focus-visible { background: rgba(var(--bb-tan-rgb), 0.06); }
  .mod.on { background: rgba(var(--bb-green-glow-rgb), 0.12); }
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
    .mod__tagline { display: none; }
    .side__head { margin-bottom: 10px; }
    .row__trigger { flex-basis: 100%; }
    .row__tags { justify-content: flex-start; }
    .list__head { padding: 14px 18px; }
  }
</style>
