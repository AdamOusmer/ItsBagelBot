<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner, Card, LightField } from '@bagel/kit';
  import PublicNav from '$lib/components/public/PublicNav.svelte';
  import PublicFooter from '$lib/components/public/PublicFooter.svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  // One flat directory of everything a viewer can type. Custom commands carry
  // their own detail (aliases, access, cooldown); module and built-in commands
  // come from the catalog and carry a usage line instead, with the module that
  // owns them as the source tag.
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
    { id: 'all', label: 'All', heading: 'Everything you can type' },
    { id: 'custom', label: 'Custom', heading: 'Custom commands' },
    { id: 'module', label: 'Modules', heading: 'Module commands' },
    { id: 'builtin', label: 'Built-in', heading: 'Built-in commands' }
  ];

  let query = $state('');
  let filter = $state<Filter>('all');
  let moduleId = $state<string | null>(null);
  let copied = $state<string | null>(null);

  const creatorCode = $derived(String(data.creatorCode ?? '').trim());

  const all = $derived.by((): Row[] => {
    const custom: Row[] = data.commands.map((c) => ({
      ...c,
      key: `custom:${c.trigger}`,
      kind: 'custom',
      source: 'Custom',
      moduleId: null
    }));
    const fromModules: Row[] = data.modules.flatMap((m) =>
      m.commands.map((c) => ({
        key: `${m.id}:${c.label}`,
        trigger: c.label,
        aliases: [],
        response: c.meta,
        perm: '',
        cooldown: 0,
        liveOnly: false,
        uses: '',
        kind: m.category === 'Built-in' ? 'builtin' : 'module',
        source: m.label,
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

  const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`;
  const summary = $derived(
    `${plural(data.commands.length, 'custom command')} · ${plural(data.modules.length, 'active module')} · ${all.length} things to type`
  );

  function pickFilter(id: Filter) {
    filter = id;
    moduleId = null;
  }

  function pickModule(id: string) {
    moduleId = moduleId === id ? null : id;
    filter = 'all';
  }

  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  function copy(text: string, key: string) {
    navigator.clipboard?.writeText(text).catch(() => {});
    clearTimeout(copyTimer);
    copied = key;
    copyTimer = setTimeout(() => (copied = null), 1400);
  }
</script>

<svelte:head>
  <title>{data.channelName} commands - ItsBagelBot</title>
  <meta
    name="description"
    content={`Active chat commands and modules for ${data.channelName} on ItsBagelBot.`}
  />
</svelte:head>

<!-- Shared public chrome (see lib/components/public): the marketing nav + footer,
     and the same drifting mote field the leaderboard and stats pages wear. -->
<PublicNav />
<div class="grain" aria-hidden="true"></div>
<div class="starfield" aria-hidden="true"><LightField /></div>
<div class="glow" aria-hidden="true"></div>

<main class="page">
  <header class="hero">
    <div class="hero__text">
      <span class="eyebrow">Channel commands</span>
      <h1 class="title">{data.channelName}</h1>
      <p class="summary">{summary}</p>
    </div>
    {#if creatorCode}
      <button class="creator" type="button" title="Copy creator code" onclick={() => copy(creatorCode, 'cc')}>
        <span class="creator__label">Creator code</span>
        <span class="creator__row">
          <strong>{creatorCode}</strong>
          <span class="creator__hint bb-chip bb-chip--muted" class:is-done={copied === 'cc'}>{copied === 'cc' ? 'Copied' : 'Click to copy'}</span>
        </span>
      </button>
    {/if}
  </header>

  {#if data.degraded}
    <div class="notice">
      <AlertBanner variant="warn">Command data is temporarily unavailable.</AlertBanner>
    </div>
  {/if}

  <div class="toolbar">
    <label class="search">
      <svg aria-hidden="true" viewBox="0 0 24 24" width="16" height="16">
        <circle cx="11" cy="11" r="7"></circle>
        <path d="m20 20-3.5-3.5"></path>
      </svg>
      <span class="sr-only">Search commands</span>
      <input type="search" bind:value={query} placeholder="Search a command or what it does…" />
    </label>
    <div class="bb-tabs" role="tablist" aria-label="Command source">
      {#each FILTERS as f (f.id)}
        {@const on = !moduleId && filter === f.id}
        <button class="bb-tab" class:is-active={on} role="tab" type="button" aria-selected={on} onclick={() => pickFilter(f.id)}>
          <span>{f.label}</span>
          <span class="tab__count">{countOf(f.id)}</span>
        </button>
      {/each}
    </div>
  </div>

  <div class="columns">
    <section class="list-wrap" aria-label="Commands">
      <Card atmosphere class="list">
        <div class="list__head">
          <span>{listHeading}</span>
          <span>Click a row to copy</span>
        </div>

        {#if rows.length}
          <ul class="rows">
            {#each rows as row (row.key)}
              <li>
                <button class="row" type="button" onclick={() => copy(row.trigger, row.key)}>
                  <span class="row__trigger">
                    <code>{row.trigger}</code>
                    {#if row.aliases.length}
                      <span class="row__aliases">{row.aliases.join(' ')}</span>
                    {/if}
                  </span>
                  <p class="row__response">{row.response}</p>
                  <span class="row__tags">
                    {#if copied === row.key}
                      <span class="copied">Copied</span>
                    {/if}
                    <span class="bb-tag bb-tag--alpha">{row.source}</span>
                    {#if row.perm}
                      <span class="bb-tag bb-tag--bare">{row.perm}</span>
                    {/if}
                    {#if row.cooldown > 0}
                      <span class="bb-tag bb-tag--bare" title="Cooldown">
                        <svg aria-hidden="true" viewBox="0 0 24 24" width="11" height="11">
                          <circle cx="12" cy="12" r="9"></circle>
                          <path d="M12 7v5l3 2"></path>
                        </svg>
                        {row.cooldown}s
                      </span>
                    {/if}
                    {#if row.liveOnly}
                      <span class="bb-tag bb-tag--live"><i class="bb-mark" aria-hidden="true"></i>Live only<i class="bb-sweep" aria-hidden="true"></i></span>
                    {/if}
                    {#if row.uses}
                      <span class="uses">{row.uses} uses</span>
                    {/if}
                  </span>
                </button>
              </li>
            {/each}
          </ul>
        {:else if all.length === 0}
          <div class="empty">No active commands yet.</div>
        {:else}
          <div class="empty">Nothing matches “{query}”. Try a shorter word.</div>
        {/if}
      </Card>
    </section>

    <aside class="side">
      <Card atmosphere class="modules">
        <div class="side__head">
          <span>Active modules</span>
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
                  <i class="bb-mark" aria-hidden="true"></i>
                  <span class="mod__text">
                    <span class="mod__label">{mod.label}</span>
                    <span class="mod__tagline">{mod.tagline}</span>
                  </span>
                  <span class="mod__meta">{n ? `${n} ${n === 1 ? 'cmd' : 'cmds'}` : 'auto'}</span>
                </button>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="side__empty">No active modules.</p>
        {/if}
      </Card>

      <div class="legend">
        <span class="side__head">Reading the tags</span>
        <p>
          <em>Everyone</em>, <em>Subs</em>, <em>Mods</em> say who can run it. The clock is the cooldown
          between uses. <em class="live">Live only</em> commands answer while the stream is up.
        </p>
      </div>
    </aside>
  </div>
</main>

<PublicFooter />

<style>
  h1, p { margin: 0; }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  /* ── atmosphere ── */

  /* Film grain over everything, nav included. Pointer-events off so it never
     eats a click; the opacity is low enough to read as texture, not fog. */
  .grain {
    position: fixed;
    inset: 0;
    z-index: 100;
    pointer-events: none;
    opacity: 0.055;
    background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
  }

  /* Mote field below content (z-index 1), the same stacking the leaderboard
     and stats pages use. */
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  /* Hearth glow behind the hero: tan core, green fringe. */
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

  /* ── page shell ── */

  .page {
    position: relative;
    z-index: 1;
    max-width: 1080px;
    margin: 0 auto;
    padding: calc(var(--bb-nav-height, 76px) + env(safe-area-inset-top, 0px) + 72px) 24px 96px;
    color: var(--bb-white);
  }

  /* ── hero ── */

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
  /* Was bare muted text; it is the copy trigger's confirmation, so it wears a
     .bb-chip frame and flips to .is-done. Only the type scale stays local. */
  .creator__hint { font-size: 10.5px; letter-spacing: 0.1em; text-transform: uppercase; }

  .notice { margin-bottom: 24px; }

  /* ── toolbar: search + source tabs, pinned under the nav ── */

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

  .search {
    position: relative;
    flex: 1 1 260px;
    min-width: 0;
  }
  .search svg {
    position: absolute;
    left: 14px;
    top: 50%;
    transform: translateY(-50%);
    fill: none;
    stroke: var(--bb-muted);
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
    pointer-events: none;
  }
  .search input {
    width: 100%;
    height: 42px;
    padding: 0 14px 0 40px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: var(--bb-card-bg);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 14px;
    outline: none;
    transition: border-color 180ms, box-shadow 180ms;
  }
  .search input::placeholder { color: var(--bb-muted); }
  .search input:focus {
    border-color: rgba(82, 183, 136, 0.6);
    box-shadow: 0 0 0 1px rgba(82, 183, 136, 0.35), 0 0 24px rgba(82, 183, 136, 0.18);
  }

  /* The rail wraps on narrow toolbars; .bb-tabs is inline-flex by default. */
  .bb-tabs { display: flex; flex-wrap: wrap; }
  .tab__count { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); }

  /* ── columns ── */

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

  /* Shared Card, re-shaped: the list is a table so its padding goes to the
     rows; the modules panel keeps a plate. Both take the 16px public radius. */
  .list-wrap :global(.card), .side :global(.card) {
    border-radius: var(--bb-radius-md);
    box-shadow: 0 1px 0 rgba(255, 255, 255, 0.02) inset, 0 8px 30px rgba(0, 0, 0, 0.35);
  }
  .list-wrap :global(.card) { --card-pad: 0; }
  .side :global(.card) { --card-pad: 22px; }

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

  /* ── one command row ── */

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
  .row__trigger code {
    font-family: var(--bb-font-mono);
    font-size: 14.5px;
    font-weight: 500;
    color: var(--bb-green-glow);
    overflow-wrap: anywhere;
  }
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

  /* The cooldown clock rides inside a .bb-tag, which sets no svg presentation. */
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

  /* ── modules panel ── */

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
  .mod :global(.bb-mark) { color: var(--bb-green-glow); }
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
  .legend p {
    font-family: var(--bb-font-body);
    font-size: 13px;
    line-height: 1.55;
    color: var(--bb-muted);
  }
  .legend em { font-style: normal; color: var(--bb-tan-light); }
  .legend em.live { color: var(--bb-green-glow); }

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
