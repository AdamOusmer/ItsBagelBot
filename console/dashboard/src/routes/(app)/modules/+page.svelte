<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, Switch, PageHead, SaveStatus, AlertBanner, EmptyState, toast, getI18n, type ModuleState } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  let { data } = $props();

  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let items = $state<ModuleState[]>(data.modules ?? []);
  // svelte-ignore state_referenced_locally
  let seed = data.modules;
  $effect(() => {
    if (data.modules !== seed) {
      seed = data.modules;
      items = data.modules ?? [];
    }
  });

  const activeCount = $derived(items.filter((m) => m.enabled).length);

  // Per-tile save indicator for the quick toggle.
  let modStatus = $state<Record<string, SaveState>>({});
  const timers = new Map<string, ReturnType<typeof setTimeout>[]>();
  function setStatus(id: string, s: SaveState) {
    for (const tm of timers.get(id) ?? []) clearTimeout(tm);
    timers.delete(id);
    modStatus = { ...modStatus, [id]: s };
  }
  function ackSaved(id: string) {
    setStatus(id, 'saved');
    timers.set(id, [
      setTimeout(() => (modStatus = { ...modStatus, [id]: 'live' }), 2500),
      setTimeout(() => (modStatus = { ...modStatus, [id]: 'idle' }), 7000)
    ]);
  }

  // Quick on/off straight from the tile (no need to open the module). Optimistic
  // flip with rollback; the config rides along so the toggle never wipes it.
  const toggleSubmit =
    (m: ModuleState): SubmitFunction =>
    () => {
      const was = m.enabled;
      items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: !was } : x));
      setStatus(m.def.id, 'saving');
      return async ({ result }) => {
        const payload =
          result.type === 'success' || result.type === 'failure'
            ? (result.data as { ok?: boolean } | undefined)
            : undefined;
        if (result.type === 'success' && payload?.ok) {
          ackSaved(m.def.id);
        } else {
          items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: was } : x));
          setStatus(m.def.id, 'error');
          timers.set(m.def.id, [setTimeout(() => (modStatus = { ...modStatus, [m.def.id]: 'idle' }), 4000)]);
          toast('err', t('modules.couldNotToggle', { label: m.def.label }));
        }
      };
    };

  // Search and Categorization
  let searchQuery = $state('');

  const allCategories = $derived(
    Array.from(new Set(items.map(m => m.def.category)))
  );

  const filteredItems = $derived(
    items.filter(m => {
      const q = searchQuery.toLowerCase();
      return m.def.label.toLowerCase().includes(q) || 
             m.def.tagline.toLowerCase().includes(q) || 
             m.def.description.toLowerCase().includes(q);
    })
  );

  const itemsByCategory = $derived(
    allCategories.map(cat => ({
      name: cat,
      id: 'cat-' + cat.toLowerCase().replace(/[^a-z0-9]+/g, '-'),
      modules: filteredItems.filter(m => m.def.category === cat)
    })).filter(c => c.modules.length > 0)
  );

  // Scrollspy logic
  let root = $state<HTMLElement | null>(null);
  let currentCategoryId = $state('');

  onMount(() => {
    if (!root) return;
    
    function update() {
      if (!root) return;
      const sections = Array.from(root.querySelectorAll<HTMLElement>('[data-category-section]'));
      if (sections.length === 0) return;
      
      let currentId = sections[0].id;
      for (const section of sections) {
        if (section.getBoundingClientRect().top < window.innerHeight * 0.35) {
          currentId = section.id;
        }
      }
      currentCategoryId = currentId;
    }

    let ticking = false;
    function onScroll() {
      if (ticking) return;
      ticking = true;
      requestAnimationFrame(() => {
        update();
        ticking = false;
      });
    }

    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onScroll, { passive: true });
    
    // Initial call after elements are likely rendered
    setTimeout(update, 50);

    return () => {
      window.removeEventListener('scroll', onScroll);
      window.removeEventListener('resize', onScroll);
    };
  });
</script>

<section class="screen active" bind:this={root}>
  <PageHead
    eyebrow={t('modules.eyebrow')}
    description={t('modules.description', { active: activeCount, total: items.length })}
  >{t('modules.titlePre')}<em>{t('modules.titleEm')}</em></PageHead>

  {#if data.degraded}
    <AlertBanner>{t('modules.degraded')}</AlertBanner>
  {/if}

  <div class="search-bar">
    <Icon name="search" size={16} />
    <input
      type="text"
      bind:value={searchQuery}
      placeholder="Search modules..."
      aria-label="Search modules"
    />
    {#if searchQuery}
      <button type="button" class="clear" aria-label="Clear search" onclick={() => (searchQuery = '')}>
        <Icon name="x" size={12} />
      </button>
    {/if}
  </div>

  <div class="layout-grid">
    <nav class="sidebar" aria-label="Module categories">
      <span class="sidebar-label">Categories</span>
      <ul>
        {#each itemsByCategory as cat (cat.id)}
          <li>
            <a 
              href="#{cat.id}" 
              class={currentCategoryId === cat.id ? 'is-current' : ''}
              onclick={(e) => {
                currentCategoryId = cat.id;
                // Let the native smooth scroll handle the jump
              }}
            >
              {cat.name}
            </a>
          </li>
        {/each}
      </ul>
    </nav>

    <div class="content">
      {#each itemsByCategory as cat (cat.id)}
        <div class="category-section" id={cat.id} data-category-section>
          <h2 class="category-title">{cat.name}</h2>
          <div class="grid">
            {#each cat.modules as m (m.def.id)}
              <div class="tile {m.enabled ? 'on' : 'off'}">
                <div class="tile-head">
                  <span class="tile-icon"><Icon name={m.def.icon} size={20} /></span>
                  <div class="tile-heading">
                    <h3 class="tile-label">{m.def.label}</h3>
                    <p class="tile-cat">{m.def.category}</p>
                  </div>
                  <span class="tile-status" data-on={m.enabled}>
                    {m.enabled ? t('modules.stateEnabled') : t('modules.stateDisabled')}
                  </span>
                </div>
                <p class="tile-purpose">{m.def.tagline}</p>
                <div class="tile-foot">
                  <a class="configure" href={m.def.href ?? `/modules/${m.def.id}`}><Icon name="settings" size={13} /> {t('modules.configure')}</a>
                  <span class="grow"></span>
                  <SaveStatus state={modStatus[m.def.id] ?? 'idle'} />
                  <form method="POST" action="?/toggle" use:enhance={toggleSubmit(m)}>
                    <input type="hidden" name="name" value={m.def.id} />
                    <input type="hidden" name="is_enabled" value={m.enabled ? '' : 'on'} />
                    <Switch
                      type="submit"
                      checked={m.enabled}
                      label={m.enabled ? t('modules.disableAria', { label: m.def.label }) : t('modules.enableAria', { label: m.def.label })}
                      pending={(modStatus[m.def.id] ?? 'idle') === 'saving'}
                    />
                  </form>
                </div>
              </div>
            {/each}
          </div>
        </div>
      {/each}

      {#if itemsByCategory.length === 0}
        <Card style="padding:0"><EmptyState icon="search" title="No modules match your search." /></Card>
      {/if}
    </div>
  </div>
</section>

<style>
  .search-bar {
    position: relative;
    margin-bottom: 32px;
    max-width: 400px;
    display: flex;
    align-items: center;
  }

  .search-bar > :global(svg) {
    position: absolute;
    left: 14px;
    color: var(--bb-muted);
    pointer-events: none;
  }

  .search-bar input {
    width: 100%;
    padding: 12px 40px 12px 42px;
    border-radius: 8px;
    border: 1px solid var(--bb-border, rgba(255, 255, 255, 0.08));
    background: rgba(240, 236, 228, 0.03);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 14px;
    transition: border-color var(--bb-dur-fast, 140ms) ease, box-shadow var(--bb-dur-fast, 140ms) ease;
  }

  .search-bar input:focus {
    outline: none;
    border-color: var(--bb-tan, #c9a87c);
    box-shadow: 0 0 0 3px rgba(201, 168, 124, 0.1);
  }

  .search-bar input::placeholder {
    color: var(--bb-muted);
  }

  .search-bar .clear {
    position: absolute;
    right: 6px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border: none;
    background: transparent;
    color: var(--bb-muted);
    cursor: pointer;
    border-radius: 8px;
    transition: color var(--bb-dur-fast, 140ms) ease;
  }

  .search-bar .clear:hover {
    color: var(--bb-white);
  }

  .layout-grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 48px;
  }

  @media (min-width: 980px) {
    .layout-grid {
      grid-template-columns: 220px minmax(0, 1fr);
      gap: 72px;
    }
  }

  .sidebar { display: none; }

  @media (min-width: 980px) {
    .sidebar {
      display: block;
      position: sticky;
      top: calc(var(--nav-offset, 76px) + 32px);
      align-self: start;
    }
  }

  .sidebar-label {
    display: block;
    font-family: var(--bb-font-mono, "DM Mono", monospace);
    font-size: 0.64rem;
    letter-spacing: 0.2em;
    text-transform: uppercase;
    color: var(--bb-muted, #888077);
    margin-bottom: 16px;
  }

  .sidebar ul {
    display: flex;
    flex-direction: column;
    gap: 2px;
    border-left: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    list-style: none;
    padding: 0;
    margin: 0;
  }

  .sidebar a {
    position: relative;
    display: block;
    padding: 7px 0 7px 16px;
    font-family: var(--bb-font-body, "DM Sans", sans-serif);
    font-size: 0.82rem;
    color: var(--bb-muted, #888077);
    text-decoration: none;
    transition: color 200ms ease, padding-left 300ms var(--ease-out-expo, cubic-bezier(0.19, 1, 0.22, 1));
  }

  .sidebar a::before {
    content: "";
    position: absolute;
    left: -1px;
    top: 20%;
    bottom: 20%;
    width: 1px;
    background: var(--bb-tan, #c9a87c);
    transform: scaleY(0);
    transition: transform 300ms var(--ease-out-expo, cubic-bezier(0.19, 1, 0.22, 1));
  }

  @media (hover: hover) and (pointer: fine) {
    .sidebar a:hover { color: var(--bb-tan-light, #e0c49a); }
  }

  .sidebar a.is-current {
    color: var(--bb-tan-light, #e0c49a);
    padding-left: 20px;
  }

  .sidebar a.is-current::before { transform: scaleY(1); }

  .content {
    display: flex;
    flex-direction: column;
    gap: 48px;
  }

  .category-section {
    scroll-margin-top: calc(var(--nav-offset, 76px) + 24px);
  }

  .category-title {
    font-family: var(--bb-font-display, "Syne", sans-serif);
    font-size: 1.25rem;
    font-weight: 700;
    color: var(--bb-white, #f0ece4);
    letter-spacing: -0.01em;
    margin: 0 0 20px 0;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 14px;
  }

  .tile {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 18px;
    border: 1px solid var(--glass-border, rgba(255, 255, 255, 0.08));
    border-radius: 8px;
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.03), rgba(240, 236, 228, 0.012));
    transition: border-color var(--bb-dur-fast, 140ms) ease;
  }
  .tile:hover { border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35)); }

  .tile-head { display: flex; align-items: flex-start; gap: 12px; }
  .tile-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    flex: none;
    border-radius: 8px;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
  }
  .tile-heading { display: flex; flex-direction: column; gap: 2px; min-width: 0; margin-right: auto; }
  .tile-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 16px; color: var(--bb-white); margin: 0; }
  .tile-cat { font-family: var(--bb-font-mono); font-size: 10.5px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--bb-muted); margin: 0; }

  /* Enabled/disabled shown as a text badge, not by dimming the card (keeps the
     card text above 4.5:1 contrast). */
  .tile-status {
    flex: none;
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    padding: 4px 8px;
    border-radius: 4px;
    border: 1px solid var(--glass-border);
    color: var(--bb-muted);
  }
  .tile-status[data-on='true'] {
    color: var(--bb-status-success, #52b788);
    border-color: var(--bb-status-success-border, rgba(82, 183, 136, 0.4));
    background: var(--bb-status-success-bg, rgba(82, 183, 136, 0.1));
  }

  .tile-purpose {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
    margin: 0;
    flex: 1;
  }

  .tile-foot { display: flex; align-items: center; gap: 10px; margin-top: auto; }
  .grow { flex: 1; }
  .configure {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-pill, 999px);
    background: rgba(255, 255, 255, 0.03);
    text-decoration: none;
    transition: color var(--bb-dur-fast, 140ms) ease, border-color var(--bb-dur-fast, 140ms) ease, background var(--bb-dur-fast, 140ms) ease;
  }
  .configure:hover { color: var(--bb-tan-pale); border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35)); background: rgba(201, 168, 124, 0.08); }
  .configure :global(svg) { stroke: currentColor; fill: none; }
</style>

