<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, Card, PageHead } from '@bagel/shared';
  import type { ModuleState } from '@bagel/shared';
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

  // Optimistically flip the local toggle; the form posts the new value.
  function flip(id: string) {
    items = items.map((m) => (m.def.id === id ? { ...m, enabled: !m.enabled } : m));
  }
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
            <span class="grow"></span>
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <form
              method="POST"
              action="?/toggle"
              use:enhance={() => async () => { /* optimistic; no invalidate */ }}
            >
              <input type="hidden" name="name" value={m.def.id} />
              <input type="hidden" name="config" value={JSON.stringify(m.config)} />
              <input type="hidden" name="is_enabled" value={m.enabled ? '' : 'on'} />
              <button
                class="toggle {m.enabled ? 'on' : ''}"
                type="submit"
                aria-label="Toggle {m.def.label}"
                onclick={() => flip(m.def.id)}
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
