<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Palette chip that opens a small panel: insert an existing counter's token
  // at the cursor, or create a counter (name + scope) right here and insert
  // it. The list lazy-loads from /counters/list on first open; create posts
  // through the counters page's own ?/create action.
  import { deserialize } from '$app/forms';
  import { Icon, getI18n, type CounterScope } from '@bagel/shared';
  import PickerPanel from '$lib/components/PickerPanel.svelte';

  const { t } = getI18n();

  // The list endpoint sends names and scopes only (values are channel
  // metrics and stay on the counters page).
  type CounterRef = { name: string; scope: CounterScope };

  let { onInsert }: { onInsert: (token: string) => void } = $props();

  let open = $state(false);
  let btnEl = $state<HTMLButtonElement>();
  let loaded = $state(false);
  let loading = $state(false);
  let counters = $state<CounterRef[]>([]);
  let newName = $state('');

  // "Who does this count for?" is one question, so it is one control. It used
  // to be two: a scope <select> plus a separate "count for the viewer they
  // mention" checkbox, which split a single decision across two widgets and
  // left the reader to work out how they combined.
  //
  // 'target' is a pseudo-scope. On the wire it is still a viewer-scoped counter
  // — the difference is only which viewer the bump keys on, which is carried by
  // the {counter:target:…} token spelling rather than by the counter's own
  // scope (issue #479). Keeping it in this list is what makes the UI match how
  // an author thinks about it; the two derived values below put it back onto
  // the two axes the wire actually has.
  const COUNTS_FOR = ['channel', 'viewer', 'target', 'command', 'viewer_command'] as const;
  let countsFor = $state<(typeof COUNTS_FOR)[number]>('channel');

  const newScope = $derived<CounterScope>(countsFor === 'target' ? 'viewer' : countsFor);
  const countTarget = $derived(countsFor === 'target');
  let creating = $state(false);
  let err = $state('');

  const scopeTag: Record<CounterScope, string> = {
    channel: t('counters.tagChannel'),
    viewer: t('counters.tagViewer'),
    command: t('counters.tagCommand'),
    viewer_command: t('counters.tagViewerCommand')
  };
  const countsForLabel: Record<(typeof COUNTS_FOR)[number], string> = {
    channel: t('counters.scopeChannel'),
    viewer: t('counters.scopeViewer'),
    target: t('counters.pickerTarget'),
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
    onInsert(countTarget ? `{counter:target:${name}}` : `{counter:${name}}`);
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
  <!-- Labelled as what it does, not as the token it eventually inserts: this
       opens a menu, so it must not wear the same mono pill as the literals. -->
  <button
    type="button"
    class="picker"
    title={t('commandEditor.tokCounter')}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={toggle}
    bind:this={btnEl}
  >
    <Icon name="pulse" size={12} />
    {t('commandEditor.pickCounter')}
    <span class="caret" aria-hidden="true">▾</span>
  </button>

  <PickerPanel {open} anchor={btnEl} label={t('counters.pickerTitle')} width={280} maxHeight={360} onClose={() => (open = false)}>
    {#snippet children()}
      <!-- Sits above both lists because it governs both: it decides the token
           spelling for whatever you insert, and the scope of anything created
           below. -->
      <label class="counts-for">
        <span class="panel-title">{t('counters.fieldScope')}</span>
        <select class="search" bind:value={countsFor}>
          {#each COUNTS_FOR as s (s)}
            <option value={s}>{countsForLabel[s]}</option>
          {/each}
        </select>
      </label>
      {#if countTarget}
        <code class="preview">{'{counter:target:'}{newName || 'name'}{'}'}</code>
      {/if}

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
      {#if err}
        <small class="err" role="alert">{err}</small>
      {/if}
      <button type="button" class="create" disabled={creating} onclick={create}>
        {creating ? t('counters.creating') : t('counters.pickerCreate')}
      </button>
    {/snippet}
  </PickerPanel>
</div>

<style>
  .cp { position: relative; display: inline-flex; }

  /* Menu trigger, not a token chip. Kept identical to FetchSourcePicker's
     .picker so the two menus read as one group beside the literal pills. */
  .picker {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-muted);
    background: transparent;
    border: 1px solid var(--rule, var(--bb-border));
    border-radius: 999px;
    padding: 3px 10px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .picker:hover,
  .picker[aria-expanded='true'] {
    color: var(--bb-white);
    border-color: var(--bb-border-strong, rgba(255, 255, 255, 0.24));
    background: rgba(255, 255, 255, 0.04);
  }
  .caret { font-size: 9px; opacity: 0.7; }


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
  .opt:hover { background: var(--glass-fill-2); }
  .opt-name { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-white); }
  .opt-tag { font-family: var(--bb-font-body); font-size: 10.5px; color: var(--bb-muted); white-space: nowrap; }

  .err { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-status-error, #cf8a78); }

  /* First row of the panel: who the counter counts against. */
  .counts-for { display: flex; flex-direction: column; gap: 5px; }
  .preview {
    margin: -2px 0 2px;
    padding-left: 24px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-green-glow, #52b788);
    opacity: 0.85;
  }

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
