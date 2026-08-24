<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Palette chip that opens a small panel of saved fetch definitions and
  // inserts `{urlfetch:<name>}` at the cursor. The list lazy-loads from
  // /commands/fetches/list on first open — the CounterPicker pattern — so the
  // commands page itself never waits on the fetch service.
  import { deserialize } from '$app/forms';
  import { getI18n } from '@bagel/shared';

  const { t } = getI18n();

  // Names only: the response side references defs by name; URLs/paths stay on
  // the fetches page.
  type DefRef = { name: string };

  let { onInsert }: { onInsert: (token: string) => void } = $props();

  let open = $state(false);
  let loaded = $state(false);
  let loading = $state(false);
  let defs = $state<DefRef[]>([]);
  let err = $state('');

  async function toggle() {
    open = !open;
    if (!open || loaded) return;
    loading = true;
    try {
      const res = await fetch('/commands/fetches/list');
      const data = (await res.json()) as { defs?: DefRef[] };
      defs = data.defs ?? [];
      loaded = true;
    } catch {
      err = t('fetches.pickerLoadFailed');
    }
    loading = false;
  }

  function pick(name: string) {
    onInsert(`{urlfetch:${name}}`);
    open = false;
  }
</script>

<div class="fchip">
  <button
    type="button"
    class="var"
    title={t('commandEditor.tokUrlfetch')}
    aria-expanded={open}
    onclick={toggle}
  >{'{urlfetch:…}'}</button>

  {#if open}
    <div class="panel" role="dialog" aria-label={t('fetches.pickerExistingTitle')}>
      <p class="panel-title">{t('fetches.pickerExistingTitle')}</p>
      {#if loading}
        <p class="mut" role="status">{t('common.loading')}</p>
      {:else if err}
        <small class="err" role="alert">{err}</small>
      {:else if defs.length === 0}
        <p class="mut">{t('fetches.pickerNoneYet')}</p>
      {:else}
        <ul class="opts">
          {#each defs.toSorted((a, b) => a.name.localeCompare(b.name)) as d (d.name)}
            <li>
              <button type="button" class="opt" onclick={() => pick(d.name)}>
                <span class="opt-name">{`{urlfetch:${d.name}}`}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      <a class="manage" href="/commands/fetches">{t('fetches.manageLink')}</a>
    </div>
  {/if}
</div>

<style>
  .fchip { position: relative; display: inline-flex; }

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
    max-height: 280px;
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

  .mut { margin: 0; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .opts { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .opt {
    width: 100%;
    display: flex;
    padding: 5px 8px;
    background: transparent;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    text-align: left;
  }
  .opt:hover { background: var(--glass-fill-2); }
  .opt-name { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-white); }

  .manage {
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-tan);
    text-decoration: none;
  }
  .manage:hover { color: var(--bb-tan-pale); text-decoration: underline; }

  .err { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-status-error, #cf8a78); }
</style>
