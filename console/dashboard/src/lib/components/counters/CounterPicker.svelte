<script lang="ts">
  // Palette chip that opens a small panel: insert an existing counter's token
  // at the cursor, or create a counter (name + scope) right here and insert
  // it. The list lazy-loads from /counters/list on first open; create posts
  // through the counters page's own ?/create action.
  import { deserialize } from '$app/forms';
  import { getI18n, COUNTER_SCOPES, type CounterScope } from '@bagel/shared';

  const { t } = getI18n();

  // The list endpoint sends names and scopes only (values are channel
  // metrics and stay on the counters page).
  type CounterRef = { name: string; scope: CounterScope };

  let { onInsert }: { onInsert: (token: string) => void } = $props();

  let open = $state(false);
  let loaded = $state(false);
  let loading = $state(false);
  let counters = $state<CounterRef[]>([]);
  let newName = $state('');
  let newScope = $state<CounterScope>('channel');
  let creating = $state(false);
  let err = $state('');

  const scopeTag: Record<CounterScope, string> = {
    channel: t('counters.tagChannel'),
    viewer: t('counters.tagViewer'),
    command: t('counters.tagCommand'),
    viewer_command: t('counters.tagViewerCommand')
  };
  const scopeLabel: Record<CounterScope, string> = {
    channel: t('counters.scopeChannel'),
    viewer: t('counters.scopeViewer'),
    command: t('counters.scopeCommand'),
    viewer_command: t('counters.scopeViewerCommand')
  };

  async function toggle() {
    open = !open;
    if (!open || loaded) return;
    loading = true;
    try {
      const res = await fetch('/counters/list');
      const data = (await res.json()) as { counters?: CounterRef[] };
      counters = data.counters ?? [];
      loaded = true;
    } catch {
      /* the list is a convenience; creating below still works */
    }
    loading = false;
  }

  function pick(name: string) {
    onInsert(`{counter:${name}}`);
    open = false;
  }

  function norm(raw: string): string {
    return raw.trim().replace(/^!/, '').toLowerCase().slice(0, 64);
  }

  async function create() {
    const name = norm(newName);
    if (!name) {
      err = t('counters.errName');
      return;
    }
    err = '';
    creating = true;
    const body = new FormData();
    body.set('name', name);
    body.set('scope', newScope);
    try {
      const res = await fetch('/counters?/create', { method: 'POST', body });
      const r = deserialize(await res.text());
      const ok = r.type === 'success' && (r.data as { ok?: boolean } | undefined)?.ok === true;
      if (ok) {
        if (!counters.some((c) => c.name === name)) counters = [...counters, { name, scope: newScope }];
        newName = '';
        pick(name);
      } else {
        err = t('counters.toastFailed');
      }
    } catch {
      err = t('counters.toastFailed');
    }
    creating = false;
  }
</script>

<div class="cp">
  <button
    type="button"
    class="var"
    title={t('commandEditor.tokCounter')}
    aria-expanded={open}
    onclick={toggle}
  >{'{counter:…}'}</button>

  {#if open}
    <div class="panel" role="dialog" aria-label={t('counters.pickerTitle')}>
      <p class="panel-title">{t('counters.pickerExisting')}</p>
      {#if loading}
        <p class="mut" role="status">{t('common.loading')}</p>
      {:else if counters.length === 0}
        <p class="mut">{t('counters.pickerEmpty')}</p>
      {:else}
        <ul class="opts">
          {#each counters.toSorted((a, b) => a.name.localeCompare(b.name)) as c (c.name)}
            <li>
              <button type="button" class="opt" onclick={() => pick(c.name)}>
                <span class="opt-name">{c.name}</span>
                <span class="opt-tag">{scopeTag[c.scope]}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}

      <p class="panel-title new">{t('counters.pickerNew')}</p>
      <input
        class="search"
        placeholder={t('counters.fieldNamePh')}
        maxlength="64"
        bind:value={newName}
        onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), create())}
      />
      <select class="search" bind:value={newScope} aria-label={t('counters.fieldScope')}>
        {#each COUNTER_SCOPES as s}
          <option value={s}>{scopeLabel[s]}</option>
        {/each}
      </select>
      {#if err}
        <small class="err" role="alert">{err}</small>
      {/if}
      <button type="button" class="create" disabled={creating} onclick={create}>
        {creating ? t('counters.creating') : t('counters.pickerCreate')}
      </button>
    </div>
  {/if}
</div>

<style>
  .cp { position: relative; display: inline-flex; }

  /* Chip matches the ResponseEditor palette vars. */
  .var {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.08);
    border: 1px solid rgba(201, 168, 124, 0.22);
    border-radius: 999px;
    padding: 3px 10px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .var:hover { background: rgba(201, 168, 124, 0.18); color: var(--bb-white); }

  .panel {
    position: absolute;
    z-index: 30;
    top: calc(100% + 6px);
    left: 0;
    width: 260px;
    max-height: 320px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--bb-bg-1, #111);
    border: 1px solid var(--bb-border);
    border-radius: 10px;
    box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
  }

  .panel-title {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 10.5px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .panel-title.new { margin-top: 4px; padding-top: 8px; border-top: 1px solid var(--rule, var(--bb-border)); }

  .mut { margin: 0; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .opts { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .opt {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 5px 8px;
    background: transparent;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    text-align: left;
  }
  .opt:hover { background: rgba(255, 255, 255, 0.05); }
  .opt-name { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-white); }
  .opt-tag { font-family: var(--bb-font-body); font-size: 10.5px; color: var(--bb-muted); white-space: nowrap; }

  .err { font-family: var(--bb-font-body); font-size: 11.5px; color: #cf8a78; }

  .create {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-green-glow, #52b788);
    background: rgba(82, 183, 136, 0.06);
    border: 1px dashed rgba(82, 183, 136, 0.4);
    border-radius: 999px;
    padding: 5px 12px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) ease;
  }
  .create:hover:not(:disabled) { background: rgba(82, 183, 136, 0.14); }
  .create:disabled { opacity: 0.45; cursor: default; }
</style>
