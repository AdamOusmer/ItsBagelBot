<script lang="ts">
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, SaveStatus, toast } from '@bagel/shared';
  import type { ModuleState } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  let { data } = $props();

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

  // Per-module save indicator (same state machine as the commands page).
  let modStatus = $state<Record<string, SaveState>>({});
  const timers = new Map<string, ReturnType<typeof setTimeout>[]>();
  function setStatus(id: string, s: SaveState) {
    for (const t of timers.get(id) ?? []) clearTimeout(t);
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

  // Optimistic flip with rollback: the toggle applies instantly; a rejected
  // write flips it back and explains why.
  const toggleSubmit =
    (m: ModuleState): SubmitFunction =>
    () => {
      const wasEnabled = m.enabled;
      items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: !wasEnabled } : x));
      setStatus(m.def.id, 'saving');
      return async ({ result }) => {
        const payload =
          result.type === 'success' || result.type === 'failure'
            ? (result.data as { ok?: boolean; error?: string } | undefined)
            : undefined;
        if (result.type === 'success' && payload?.ok) {
          ackSaved(m.def.id);
        } else {
          items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: wasEnabled } : x));
          setStatus(m.def.id, 'error');
          timers.set(m.def.id, [setTimeout(() => (modStatus = { ...modStatus, [m.def.id]: 'idle' }), 4000)]);
          toast('err', payload?.error ?? `Could not toggle ${m.def.label}.`);
        }
      };
    };
</script>

<section class="screen active">
  <PageHead
    eyebrow="Manage"
    description="Optional features for your channel. {activeCount} of {items.length} enabled."
  >Channel <em>modules</em></PageHead>

  {#if data.degraded}
    <Card style="padding:14px 16px;margin-bottom:14px">
      <span class="degraded"><Icon name="ban" size={14} /> Module settings are temporarily unavailable. Showing defaults.</span>
    </Card>
  {/if}

  <div class="grid">
    {#each items as m (m.def.id)}
      <Card style="padding:0">
        <div class="mod {m.enabled ? '' : 'off'}">
          <a class="mod-main" href={`/modules/${m.def.id}`}>
            <span class="mod-icon"><Icon name={m.def.icon} size={18} /></span>
            <span class="mod-text">
              <span class="mod-label">{m.def.label}</span>
              <span class="mod-tag">{m.def.tagline}</span>
            </span>
          </a>
          <div class="mod-foot">
            <a class="cfg" href={`/modules/${m.def.id}`}>
              <Icon name="settings" size={13} /> Configure
            </a>
            <SaveStatus state={modStatus[m.def.id] ?? 'idle'} />
            <span class="grow"></span>
            <form method="POST" action="?/toggle" use:enhance={toggleSubmit(m)}>
              <input type="hidden" name="name" value={m.def.id} />
              <input type="hidden" name="config" value={JSON.stringify(m.config)} />
              <input type="hidden" name="is_enabled" value={m.enabled ? '' : 'on'} />
              <button
                class="toggle {m.enabled ? 'on' : ''}"
                type="submit"
                aria-label="Toggle {m.def.label}"
              ></button>
            </form>
          </div>
        </div>
      </Card>
    {/each}
  </div>

  {#if items.length === 0}
    <Card style="padding:28px 18px"><div class="empty">No modules available yet.</div></Card>
  {/if}
</section>

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 14px;
  }
  .mod { display: flex; flex-direction: column; height: 100%; transition: opacity var(--bb-dur-fast, 140ms) ease; }
  .mod.off { opacity: 0.6; }
  .mod-main {
    display: flex; gap: 12px; align-items: flex-start;
    padding: 18px 18px 12px; text-decoration: none; color: inherit;
  }
  .mod-icon {
    display: inline-flex; align-items: center; justify-content: center;
    width: 38px; height: 38px; flex: none;
    border-radius: var(--bb-radius-md, 10px);
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
  }
  .mod-text { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .mod-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 16px; color: var(--bb-white); }
  .mod-tag { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); line-height: 1.5; }
  .mod-foot {
    display: flex; align-items: center; gap: 10px;
    padding: 10px 16px 14px; margin-top: auto;
    border-top: 1px solid var(--glass-border);
  }
  .grow { flex: 1; }
  .cfg {
    display: inline-flex; align-items: center; gap: 6px;
    font-family: var(--bb-font-body); font-size: 12.5px;
    color: var(--bb-muted); text-decoration: none;
    transition: color var(--bb-dur-fast, 140ms) ease;
  }
  .cfg:hover { color: var(--bb-white); }
  .empty, .degraded { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; display: inline-flex; gap: 8px; align-items: center; }
  .empty { display: block; text-align: center; }
</style>
