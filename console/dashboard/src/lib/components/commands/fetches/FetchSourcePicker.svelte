<script module lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Shape of one saved data source as the page load and the save/delete actions
  // all return it. Lives in the module script so consumers can import the type
  // straight from the component that owns it.
  export interface SourceDef {
    name: string;
    url: string;
    json_path: string[];
    key_label: string;
  }
</script>

<script lang="ts">
  // The {urlfetch:…} palette chip and the whole data-source lifecycle behind it.
  //
  // This replaces the standalone /commands/fetches page. That page made a fetch
  // definition feel like a second product you had to go configure before you
  // could write a command; authors did not find it, and the ones who did were
  // met with a builder that asked them to paste raw JSON. Here a data source is
  // what it actually is — a variable you insert into a reply — so it is created
  // and picked from inside the command editor, and never named anywhere else.
  //
  // Two surfaces:
  //   chip -> popover  pick a saved source (inserts its token) or delete one.
  //   popover -> modal the builder: name, URL, optional key, then FETCH one
  //                    real response and click the value you want.
  //
  // The builder deliberately never asks for a "response kind". Clicking a value
  // in the tree means json + that path; skipping the tree means plain text. The
  // author answers "which value do you want?", not "what shape is your API?".
  import { deserialize } from '$app/forms';
  import { Button, Modal, getI18n, portal, slugifyName, buildJsonPath, DEFS_PER_BROADCASTER } from '@bagel/shared';
  import JsonTree from './JsonTree.svelte';

  const { t } = getI18n();

  let {
    defs = [],
    keys = [],
    onInsert,
    onDefsChanged
  }: {
    defs?: SourceDef[];
    keys?: { label: string }[];
    onInsert: (token: string) => void;
    /** Hands the server's refreshed list back to the page that owns it. */
    onDefsChanged?: (defs: SourceDef[]) => void;
  } = $props();

  let open = $state(false);
  let building = $state(false);
  let btnEl = $state<HTMLButtonElement>();
  /** Failure text for the popover itself (delete refusals), not the builder. */
  let panelErr = $state('');

  // Fixed coords computed from the trigger rect on open. The chip lives inside
  // the command editor's InspectorSurface, which sets `overflow: hidden` to clip
  // its own scroller — an absolutely-positioned panel is silently eaten by it.
  // Portalling to <body> and positioning fixed is the same escape hatch
  // InspectorSurface itself uses for its mobile sheet.
  let pos = $state({ top: 0, left: 0 });
  const PANEL_W = 300;
  const PANEL_H = 340;
  const GAP = 8;

  function place() {
    if (!btnEl) return;
    const r = btnEl.getBoundingClientRect();
    const left = r.right + GAP + PANEL_W <= window.innerWidth ? r.right + GAP : Math.max(GAP, r.left - GAP - PANEL_W);
    pos = { top: Math.max(GAP, Math.min(r.top, window.innerHeight - GAP - PANEL_H)), left };
  }

  function toggle() {
    open = !open;
    if (!open) return;
    // Clear on open, not on close: the scroll/resize handler closes without
    // going through here, so an armed delete could otherwise still be primed
    // the next time the popover appears.
    armedDelete = '';
    panelErr = '';
    place();
  }

  // Coords are a snapshot, so movement invalidates them. Closing beats
  // re-placing: the panel is transient and a drifting popover reads as a bug.
  $effect(() => {
    if (!open) return;
    const close = () => (open = false);
    window.addEventListener('scroll', close, { capture: true, passive: true });
    window.addEventListener('resize', close, { passive: true });
    return () => {
      window.removeEventListener('scroll', close, { capture: true });
      window.removeEventListener('resize', close);
    };
  });

  function tokenFor(name: string): string {
    return `{urlfetch:${name}}`;
  }

  function pick(name: string) {
    onInsert(tokenFor(name));
    open = false;
  }

  // --- builder state -------------------------------------------------------
  let displayName = $state('');
  let slug = $state('');
  let slugTouched = $state(false);
  let url = $state('');
  let keyLabel = $state('');
  let sample = $state('');
  let path = $state<string[]>([]);
  let pathPicked = $state(false);
  let fetching = $state(false);
  let creating = $state(false);
  let notice = $state('');
  let err = $state('');
  let showPaste = $state(false);

  const atQuota = $derived(defs.length >= DEFS_PER_BROADCASTER);
  const canCreate = $derived(slug !== '' && url.trim() !== '' && !creating);

  function onDisplayName() {
    if (!slugTouched) slug = slugifyName(displayName);
  }

  function openBuilder() {
    displayName = '';
    slug = '';
    slugTouched = false;
    url = '';
    keyLabel = '';
    sample = '';
    path = [];
    pathPicked = false;
    notice = '';
    err = '';
    showPaste = false;
    open = false;
    building = true;
  }

  function draftForm(): FormData {
    const f = new FormData();
    f.set('name', slug);
    f.set('url', url.trim());
    // Kind is inferred, never asked: a picked value means json + that path.
    f.set('kind', pathPicked && path.length > 0 ? 'json' : 'plain');
    f.set('path', buildJsonPath(path));
    f.set('key_label', keyLabel);
    f.set('edit', '0');
    return f;
  }

  async function post(action: string, body: FormData) {
    const res = await fetch(`/commands?/${action}`, { method: 'POST', body });
    return deserialize(await res.text());
  }

  // Fetch one real response so the author can click a value out of it. Sent
  // with an EMPTY path on purpose — we want the whole document to build a tree
  // from, and a path the author has not chosen yet would fail validation.
  async function fetchSample() {
    err = '';
    notice = '';
    fetching = true;
    try {
      const f = draftForm();
      f.set('kind', 'plain');
      f.set('path', '');
      const r = await post('testfetch', f);
      const d = (r.type === 'success' || r.type === 'failure' ? r.data : undefined) as
        | { ok?: boolean; sample?: string; status?: string; error?: string }
        | undefined;
      if (d?.sample) {
        sample = d.sample;
        showPaste = false;
        // A non-ok status still yields a body worth showing: the fetch reached
        // the API, and the tree is what lets the author fix whatever was wrong.
        if (d.status && d.status !== 'ok') notice = t(`fetches.test_${d.status}`);
      } else if (d?.ok) {
        // Reached the API, but gossip declined to hand back the body (too large
        // or not UTF-8). Paste is the honest fallback.
        notice = t('fetches.builderNoSample');
        showPaste = true;
      } else {
        err = d?.error ?? t('fetches.testNoAnswer');
        showPaste = true;
      }
    } catch {
      err = t('fetches.testNoAnswer');
      showPaste = true;
    }
    fetching = false;
  }

  function onPickPath(segs: string[]) {
    path = [...segs];
    pathPicked = true;
  }

  function useWholeResponse() {
    path = [];
    pathPicked = false;
  }

  async function create() {
    if (!canCreate) return;
    err = '';
    creating = true;
    try {
      const r = await post('savefetch', draftForm());
      const d = (r.type === 'success' || r.type === 'failure' ? r.data : undefined) as
        | { ok?: boolean; defs?: SourceDef[]; error?: string }
        | undefined;
      if (d?.ok) {
        if (d.defs) onDefsChanged?.(d.defs);
        onInsert(tokenFor(slug));
        building = false;
      } else {
        err = d?.error ?? t('fetches.toastSaveFailed');
      }
    } catch {
      err = t('fetches.toastSaveFailed');
    }
    creating = false;
  }

  // Delete is two-tap rather than one: the × arms, a second click commits.
  // There is no undo, and the old page guarded this with a full confirm dialog
  // listing the commands that quote the source — too heavy for a popover, but
  // deleting on a single stray click would be worse than either.
  let armedDelete = $state('');

  async function remove(name: string) {
    if (armedDelete !== name) {
      armedDelete = name;
      panelErr = '';
      return;
    }
    armedDelete = '';
    panelErr = '';
    const f = new FormData();
    f.set('name', name);
    try {
      const r = await post('deletefetch', f);
      const d = (r.type === 'success' || r.type === 'failure' ? r.data : undefined) as
        | { ok?: boolean; defs?: SourceDef[]; error?: string }
        | undefined;
      if (d?.ok && d.defs) {
        onDefsChanged?.(d.defs);
        return;
      }
      // The service refuses while a command still quotes the source. Saying so
      // matters more here than anywhere else: the row simply not disappearing
      // reads as a dead button.
      panelErr = d?.error ?? t('fetches.toastDeleteFailed', { name });
    } catch {
      panelErr = t('fetches.toastDeleteFailed', { name });
    }
  }
</script>

<div class="fsp">
  <button
    type="button"
    class="var"
    title={t('commandEditor.tokUrlfetch')}
    aria-expanded={open}
    onclick={toggle}
    bind:this={btnEl}>{'{urlfetch:…}'}</button
  >

  {#if open}
    <div
      class="panel"
      data-overlay
      role="dialog"
      aria-label={t('fetches.pickerExistingTitle')}
      style="top: {pos.top}px; left: {pos.left}px"
      use:portal
    >
      <p class="panel-title">{t('fetches.pickerExistingTitle')}</p>
      {#if defs.length === 0}
        <p class="mut">{t('fetches.builderNoneYet')}</p>
      {:else}
        <ul class="opts">
          {#each defs.toSorted((a, b) => a.name.localeCompare(b.name)) as d (d.name)}
            <li>
              <button type="button" class="opt" onclick={() => pick(d.name)}>
                <span class="opt-name">{tokenFor(d.name)}</span>
                <span class="opt-path">{d.json_path.length ? buildJsonPath(d.json_path) : t('fetches.kindPlain')}</span>
              </button>
              <button
                type="button"
                class="opt-del"
                class:armed={armedDelete === d.name}
                aria-label={armedDelete === d.name
                  ? t('fetches.deleteTitle', { name: d.name })
                  : t('fetches.deleteAria', { name: d.name })}
                onclick={() => remove(d.name)}>{armedDelete === d.name ? t('common.delete') : '×'}</button
              >
            </li>
          {/each}
        </ul>
      {/if}
      {#if panelErr}
        <small class="err" role="alert">{panelErr}</small>
      {/if}
      {#if atQuota}
        <small class="err">{t('fetches.quotaReached', { max: String(DEFS_PER_BROADCASTER) })}</small>
      {:else}
        <button type="button" class="new" onclick={openBuilder}>{t('fetches.builderNew')}</button>
      {/if}
    </div>
  {/if}
</div>

<Modal open={building} title={t('fetches.builderTitle')} busy={creating} closeModal={() => (building = false)}>
  <div class="build">
    <p class="intro">{t('fetches.builderIntro')}</p>

    <label class="fld">
      <span>{t('fetches.displayName')}</span>
      <input class="in" placeholder={t('fetches.displayNamePh')} bind:value={displayName} oninput={onDisplayName} />
    </label>

    <label class="fld">
      <span>{t('fetches.slug')}</span>
      <input
        class="in mono"
        bind:value={slug}
        oninput={() => {
          slugTouched = true;
          slug = slugifyName(slug);
        }}
      />
      <small class="hint">{t('fetches.slugHint')}</small>
    </label>

    <label class="fld">
      <span>{t('fetches.builderUrl')}</span>
      <input class="in mono" placeholder="https://api.example.com/v1/…" spellcheck="false" bind:value={url} />
    </label>

    {#if keys.length > 0}
      <label class="fld">
        <span>{t('fetches.auth')}</span>
        <select class="in" bind:value={keyLabel}>
          <option value="">{t('fetches.authNone')}</option>
          {#each keys.toSorted((a, b) => a.label.localeCompare(b.label)) as k (k.label)}
            <option value={k.label}>{k.label}</option>
          {/each}
        </select>
      </label>
    {/if}

    <div class="sample-row">
      <Button variant="secondary" icon="pulse" loading={fetching} disabled={url.trim() === ''} onclick={fetchSample}>
        {fetching ? t('fetches.builderFetching') : t('fetches.builderFetch')}
      </Button>
      {#if !showPaste && sample === ''}
        <button type="button" class="link" onclick={() => (showPaste = true)}>{t('fetches.builderPasteInstead')}</button>
      {/if}
    </div>

    {#if notice}<small class="notice" role="status">{notice}</small>{/if}
    {#if err}<small class="err" role="alert">{err}</small>{/if}

    {#if showPaste}
      <textarea
        class="in mono paste"
        rows="4"
        spellcheck="false"
        placeholder={t('fetches.pickerPlaceholder')}
        aria-label={t('fetches.pickerSampleAria')}
        bind:value={sample}
      ></textarea>
    {/if}

    {#if sample !== ''}
      <p class="pick-prompt">{t('fetches.builderPickPrompt')}</p>
      <JsonTree json={sample} onPick={onPickPath} leafTitle={(segs) => `${tokenFor(slug || 'name')} → ${buildJsonPath(segs)}`} />
      <div class="chosen">
        {#if pathPicked && path.length > 0}
          <span class="chosen-tag">{t('fetches.builderPicked')}</span>
          <code>{buildJsonPath(path)}</code>
          <button type="button" class="link" onclick={useWholeResponse}>{t('fetches.builderWholeResponse')}</button>
        {:else}
          <span class="mut">{t('fetches.builderWholeSelected')}</span>
        {/if}
      </div>
    {/if}

    <div class="foot">
      <Button variant="ghost" onclick={() => (building = false)}>{t('common.cancel')}</Button>
      <Button variant="primary" icon="check" loading={creating} disabled={!canCreate} onclick={create}>
        {creating ? t('fetches.builderCreating') : t('fetches.builderCreate')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .fsp { position: relative; display: inline-flex; }

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
    position: fixed;
    z-index: 300;
    width: 300px;
    max-height: 340px;
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
  :global(:root[data-theme='light']) .panel { box-shadow: 0 12px 32px rgba(20, 17, 12, 0.15); }

  .panel-title {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 10.5px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .mut { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  p.mut { margin: 0; font-style: italic; }

  .opts { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .opts li { display: flex; align-items: center; gap: 4px; }
  .opt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 5px 8px;
    background: transparent;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    text-align: left;
  }
  .opt:hover { background: var(--glass-fill-2); }
  .opt-name { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-white); }
  .opt-path {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .opt-del {
    flex: none;
    min-width: 22px;
    height: 22px;
    padding: 0 6px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--bb-muted);
    cursor: pointer;
    font-size: 14px;
    line-height: 1;
  }
  .opt-del:hover { color: var(--bb-status-error, #cf8a78); background: rgba(207, 138, 120, 0.12); }
  /* Armed state reads as the destructive commit, not a second dismiss. */
  .opt-del.armed {
    color: var(--bb-status-error, #cf8a78);
    background: rgba(207, 138, 120, 0.16);
    font-family: var(--bb-font-body);
    font-size: 11px;
  }

  .new {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-green-glow, #52b788);
    background: rgba(82, 183, 136, 0.06);
    border: 1px dashed rgba(82, 183, 136, 0.4);
    border-radius: 999px;
    padding: 5px 12px;
    cursor: pointer;
  }
  .new:hover { background: rgba(82, 183, 136, 0.14); }

  /* --- builder --- */
  .build { display: flex; flex-direction: column; gap: 12px; min-width: min(460px, 78vw); }
  .intro { margin: 0; font-family: var(--bb-font-body); font-size: 12.5px; line-height: 1.55; color: var(--bb-muted); }

  .fld { display: flex; flex-direction: column; gap: 5px; }
  .fld > span { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-tan-light); }
  .in {
    width: 100%;
    box-sizing: border-box;
    padding: 9px 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: 8px;
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
  }
  .in.mono { font-family: var(--bb-font-mono); font-size: 12px; }
  .in:focus { outline: none; border-color: rgba(82, 183, 136, 0.5); }
  .paste { resize: vertical; min-height: 74px; line-height: 1.5; }
  .hint { font-family: var(--bb-font-body); font-size: 11px; color: var(--bb-muted); }

  .sample-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  .link {
    background: none;
    border: none;
    padding: 0;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-muted);
    text-decoration: underline;
    cursor: pointer;
  }
  .link:hover { color: var(--bb-white); }

  .notice { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-tan-light); }
  .err { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-status-error, #cf8a78); }

  .pick-prompt { margin: 0; font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-tan-light); }
  .chosen { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .chosen-tag { font-family: var(--bb-font-body); font-size: 11px; color: var(--bb-muted); }
  .chosen code { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-green-glow, #52b788); }

  .foot { display: flex; justify-content: flex-end; gap: 8px; padding-top: 4px; }
</style>
