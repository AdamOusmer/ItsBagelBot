<script lang="ts">
  import { copyFlash } from '@bagel/ui/lib/clipboard';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner, Card, Code, LightField, SearchInput, Text } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
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
  let copied = $state<string | null>(null);

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
    total: all.length
  }));

  function pickFilter(id: Filter) {
    filter = id;
    moduleId = null;
  }

  function pickModule(id: string) {
    moduleId = moduleId === id ? null : id;
    filter = 'all';
  }

  let copyGeneration = 0;
  function copy(text: string, key: string) {
    const generation = ++copyGeneration;
    void copyFlash(text, (on) => {
      if (generation === copyGeneration) copied = on ? key : null;
    }, 1400);
  }

</script>

<svelte:head>
  <title>{t('public.commands.pageTitle', { channel: data.channelName })}</title>
  <meta
    name="description"
    content={t('public.commands.pageDescription', { channel: data.channelName })}
  />
</svelte:head>

<div class="starfield" aria-hidden="true"><LightField /></div>
<div class="glow" aria-hidden="true"></div>

<main class="page">
  <header class="hero">
    <div class="hero__text">
      <span class="eyebrow">{t('public.commands.eyebrow')}</span>
      <h1 class="title">{data.channelName}</h1>
      <p class="summary">{summary}</p>
    </div>
    {#if creatorCode}
      <button class="creator" type="button" title={t('public.commands.copyCreator')} onclick={() => copy(creatorCode, 'cc')}>
        <span class="creator__label">{t('public.commands.copyCreator')}</span>
        <span class="creator__row">
          <strong>{creatorCode}</strong>
          <span class="creator__hint bb-chip bb-chip--muted" class:is-done={copied === 'cc'}>{copied === 'cc' ? t('common.copied') : t('public.commands.clickToCopy')}</span>
        </span>
      </button>
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
    <div class="bb-tabs bb-tabs--wrap" role="tablist" aria-label={t('public.commands.sourceLabel')}>
      {#each FILTERS as f (f.id)}
        {@const on = !moduleId && filter === f.id}
        <button class="bb-tab" class:is-active={on} role="tab" type="button" aria-selected={on} onclick={() => pickFilter(f.id)}
          >{f.label}<span class="bb-tab__count">{countOf(f.id)}</span></button
        >
      {/each}
    </div>
  </div>

  <div class="columns">
    <section class="list-wrap" aria-label={t('public.commands.commandsLabel')}>
      <Card atmo class="list">
        <div class="list__head">
          <span>{listHeading}</span>
          <span>{t('public.commands.clickRow')}</span>
        </div>

        {#if rows.length}
          <ul class="rows">
            {#each rows as row (row.key)}
              <li>
                <button class="row" type="button" onclick={() => copy(row.trigger, row.key)}>
                  <span class="row__trigger">
                    <Code class="trigger-code">{row.trigger}</Code>
                    {#if row.aliases.length}
                      <span class="row__aliases">{row.aliases.join(' ')}</span>
                    {/if}
                  </span>
                  <p class="row__response">{row.response}</p>
                  <span class="row__tags">
                    {#if copied === row.key}
                      <span class="copied">{t('common.copied')}</span>
                    {/if}
                    <span class="bb-tag bb-tag--alpha">{row.source}</span>
                    {#if row.perm}
                      <span class="bb-tag bb-tag--bare">{row.perm}</span>
                    {/if}
                    {#if row.cooldown > 0}
                      <span class="bb-tag bb-tag--bare" title={t('public.commands.cooldown')}>
                        <svg aria-hidden="true" viewBox="0 0 24 24" width="11" height="11">
                          <circle cx="12" cy="12" r="9"></circle>
                          <path d="M12 7v5l3 2"></path>
                        </svg>
                        {row.cooldown}s
                      </span>
                    {/if}
                    {#if row.liveOnly}
                      <span class="bb-tag bb-tag--live"><i class="bb-mark" aria-hidden="true"></i>{t('public.commands.liveOnly')}<i class="bb-sweep" aria-hidden="true"></i></span>
                    {/if}
                    {#if row.uses}
                      <span class="uses">{t('public.commands.uses', { count: row.uses })}</span>
                    {/if}
                  </span>
                </button>
              </li>
            {/each}
          </ul>
        {:else if all.length === 0}
          <div class="empty">{t('public.commands.noCommands')}</div>
        {:else}
          <div class="empty">{t('public.commands.noMatches', { query })}</div>
        {/if}
      </Card>
    </section>

    <aside class="side">
      <Card atmo class="modules">
        <div class="side__head">
          <span>{t('public.commands.activeModules')}</span>
          <span class="side__count">{data.modules.length}</span>
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
                  <i class="bb-mark mod__mark" aria-hidden="true"></i>
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
          <p class="side__empty">{t('public.commands.noModules')}</p>
        {/if}
      </Card>

      <div class="legend">
        <span class="side__head">{t('public.commands.legendTitle')}</span>
        <Text size="sm" tone="muted">
          {t('public.commands.legend')}
        </Text>
      </div>
    </aside>
  </div>
</main>

<style>
  h1, p { margin: 0; }



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
      rgba(201, 168, 124, 0.18) 0%,
      rgba(82, 183, 136, 0.08) 40%,
      transparent 72%
    );
    filter: blur(18px);
  }

  .page {
    position: relative;
    z-index: 1;
    max-width: 1080px;
    margin: 0 auto;
    padding: calc(var(--bb-nav-height, 76px) + env(safe-area-inset-top, 0px) + 72px) 24px 96px;
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
  }

  .eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
    text-shadow: 0 0 16px rgba(82, 183, 136, 0.45);
  }

  .title {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(40px, 5.5vw, 64px);
    line-height: 1;
    letter-spacing: -0.03em;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }

  .summary {
    font-family: var(--bb-font-body);
    font-size: 15px;
    line-height: 1.55;
    color: var(--bb-muted);
  }

  .creator {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    padding: 14px 20px 14px 18px;
    border: 1px solid rgba(201, 168, 124, 0.35);
    border-radius: var(--bb-radius-md);
    background: var(--bb-card-bg) radial-gradient(220px 120px at 100% 0%, rgba(201, 168, 124, 0.14), transparent 70%);
    color: var(--bb-white);
    cursor: pointer;
    font: inherit;
    text-align: left;
    box-shadow: 0 1px 0 rgba(255, 255, 255, 0.02) inset, 0 8px 30px rgba(0, 0, 0, 0.35);
    transition: box-shadow 180ms, border-color 180ms;
  }
  .creator:hover, .creator:focus-visible {
    border-color: var(--bb-tan);
    box-shadow: 0 0 0 1px rgba(201, 168, 124, 0.35), 0 0 24px rgba(201, 168, 124, 0.18);
  }
  .creator__label {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-tan);
    white-space: nowrap;
  }
  .creator__row { display: flex; align-items: baseline; gap: 14px; }
  .creator__row strong {
    font-family: var(--bb-font-display);
    font-size: 24px;
    font-weight: 700;
    letter-spacing: 0.04em;
    line-height: 1;
    color: var(--bb-tan-pale);
    text-shadow: 0 0 18px rgba(201, 168, 124, 0.25);
  }
  .creator__hint { font-size: 10.5px; letter-spacing: 0.1em; text-transform: uppercase; }

  .notice { margin-bottom: 24px; }

  .toolbar {
    position: sticky;
    top: calc(var(--bb-nav-height, 76px) + env(safe-area-inset-top, 0px));
    z-index: 40;
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
    flex: 1 1 260px;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .list-wrap { --card-pad: 0; }
  .side { --card-pad: 22px; }
  .list-wrap, .side { --card-radius: var(--bb-radius-md); }
  :global(.list), :global(.modules) {
    box-shadow: 0 1px 0 rgba(255, 255, 255, 0.02) inset, 0 8px 30px rgba(0, 0, 0, 0.35);
  }

  .list__head, .side__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .list__head { padding: 16px 24px; border-bottom: 1px solid var(--bb-border); }
  .side__head { margin-bottom: 16px; }
  .side__count { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0; text-transform: none; }

  .rows, .mods { list-style: none; margin: 0; padding: 0; }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 8px 28px;
    width: 100%;
    padding: 18px 24px;
    border: 0;
    border-top: 1px solid rgba(201, 168, 124, 0.08);
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    transition: background 180ms;
  }
  .rows li:first-child .row { border-top: 0; }
  .row:hover, .row:focus-visible { background: rgba(201, 168, 124, 0.045); }
  .row:focus-visible { outline: 1px solid rgba(82, 183, 136, 0.6); outline-offset: -1px; }

  .row__trigger {
    flex: 0 0 168px;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  :global(.trigger-code) { font-size: 14.5px; font-weight: 500; color: var(--bb-green-glow); }
  .row__aliases {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    overflow-wrap: anywhere;
  }
  .row__response {
    flex: 1 1 240px;
    min-width: 0;
    font-family: var(--bb-font-body);
    font-size: 14.5px;
    line-height: 1.6;
    color: var(--bb-white);
    overflow-wrap: anywhere;
    text-wrap: pretty;
  }
  .row__tags {
    flex: 0 0 auto;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
    padding-top: 2px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
  }

  .row__tags svg { fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
  .uses { color: var(--bb-muted); white-space: nowrap; padding-left: 4px; }
  .copied { color: var(--bb-green-glow); animation: fadeIn 180ms ease-out; }

  .empty {
    padding: 40px 20px;
    text-align: center;
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: var(--bb-muted);
  }

  .side :global(.modules) {
    background-image: radial-gradient(240px 140px at 100% 0%, rgba(82, 183, 136, 0.08), transparent 70%);
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
    transition: background 180ms;
  }
  .mod:disabled { cursor: default; }
  .mod:not(:disabled):hover, .mod:focus-visible { background: rgba(201, 168, 124, 0.06); }
  .mod.on { background: rgba(82, 183, 136, 0.12); }
  .mod__mark { color: var(--bb-green-glow); }
  .mod__text { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .mod__label { font-family: var(--bb-font-body); font-size: 14px; font-weight: 600; color: var(--bb-white); }
  .mod__tagline {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.4;
    color: var(--bb-muted);
    text-wrap: pretty;
  }
  .mod__meta { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-tan); white-space: nowrap; }

  .side__empty { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }

  .legend {
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    padding: 20px 22px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .legend .side__head { margin-bottom: 0; }

  @keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

  @media (max-width: 640px) {
    .page { padding-left: 16px; padding-right: 16px; }
    .row { padding: 16px 18px; }
    .row__trigger { flex-basis: 100%; }
    .row__tags { justify-content: flex-start; }
    .list__head { padding: 14px 18px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .copied { animation: none; }
  }
</style>
