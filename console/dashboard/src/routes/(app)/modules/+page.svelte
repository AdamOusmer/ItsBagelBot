<script lang="ts">
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, SaveStatus, toast, getI18n, type ModuleState } from '@bagel/shared';
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
</script>

<section class="screen active">
  <PageHead
    eyebrow={t('modules.eyebrow')}
    description={t('modules.description', { active: activeCount, total: items.length })}
  >{t('modules.titlePre')}<em>{t('modules.titleEm')}</em></PageHead>

  {#if data.degraded}
    <div class="degraded" role="alert"><Icon name="ban" size={13} /> {t('modules.degraded')}</div>
  {/if}

  <!-- Tiles: the body opens the module's page; the foot has a quick on/off. -->
  <div class="grid">
    {#each items as m (m.def.id)}
      <div class="tile {m.enabled ? 'on' : 'off'}">
        <a class="tile-main" href={`/modules/${m.def.id}`}>
          <span class="tile-icon"><Icon name={m.def.icon} size={20} /></span>
          <span class="tile-text">
            <span class="tile-label">{m.def.label}</span>
            <span class="tile-tag">{m.def.tagline}</span>
          </span>
        </a>
        <div class="tile-foot">
          <a class="open" href={`/modules/${m.def.id}`}><Icon name="settings" size={13} /> {t('modules.configure')}</a>
          <span class="grow"></span>
          <SaveStatus state={modStatus[m.def.id] ?? 'idle'} />
          <form method="POST" action="?/toggle" use:enhance={toggleSubmit(m)}>
            <input type="hidden" name="name" value={m.def.id} />
            <input type="hidden" name="config" value={JSON.stringify(m.config)} />
            <input type="hidden" name="is_enabled" value={m.enabled ? '' : 'on'} />
            <button
              class="toggle {m.enabled ? 'on' : ''}"
              type="submit"
              aria-label={t('modules.toggleAria', { label: m.def.label })}
            ></button>
          </form>
        </div>
      </div>
    {/each}
  </div>

  {#if items.length === 0}
    <Card style="padding:28px 18px"><div class="empty">{t('modules.empty')}</div></Card>
  {/if}
</section>

<style>
  .degraded {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
    padding: 10px 14px;
    border: 1px solid rgba(176, 90, 70, 0.4);
    border-radius: 8px 8px;
    background: rgba(176, 90, 70, 0.08);
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13px;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 14px;
  }

  .tile {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--glass-border, rgba(255, 255, 255, 0.08));
    border-radius: 8px 8px;
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.03), rgba(240, 236, 228, 0.012));
    transition: border-color var(--bb-dur-fast, 140ms) ease, background var(--bb-dur-fast, 140ms) ease;
  }
  .tile:hover { border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35)); }
  .tile.off .tile-main { opacity: 0.6; }

  .tile-main {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 18px 18px 14px;
    text-decoration: none;
    color: inherit;
    transition: opacity var(--bb-dur-fast, 140ms) ease;
  }
  .tile-main:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -2px; }

  .tile-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    flex: none;
    border-radius: 8px 8px;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
  }
  .tile-text { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .tile-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 16px; color: var(--bb-white); }
  .tile-tag { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); line-height: 1.5; }

  .tile-foot {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 16px 14px;
    margin-top: auto;
    border-top: 1px solid var(--glass-border);
  }
  .grow { flex: 1; }
  .open {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    text-decoration: none;
    transition: color var(--bb-dur-fast, 140ms) ease;
  }
  .open:hover { color: var(--bb-white); }

  .empty { text-align: center; color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; }
</style>
