<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Paste-a-sample JSON path picker for fetch definitions: the author pastes
  // one real API response, this renders it as a tree, and clicking a leaf
  // inserts the full `{urlfetch:name.dot.path}` token at the editor's caret
  // through the caller's onInsert — never a clipboard round-trip (the
  // CounterPicker's create-or-pick shape).
  //
  // Zero dependencies: the tree is a recursive Svelte 5 snippet over a plain
  // node list built from one guarded JSON.parse. The grammar enforced here is
  // the Go resolver's — segments [A-Za-z0-9_-]+ (array indices arrive as bare
  // digits), depth <= 8 — so a path picked here always parses server-side.
  import {
    JSON_PATH_MAX_DEPTH,
    buildJsonPath,
    getI18n,
    parseJsonPath,
    slugifyName
  } from '@bagel/shared';

  const { t } = getI18n();

  // Pasted responses are capped well above any real single-value payload:
  // gossip itself refuses upstream bodies past 1 MiB post-decompression, and
  // typical JSON APIs answer in tens of KB. 128 KB keeps JSON.parse plus the
  // rendered tree inside the docked inspector's interaction budget (measured
  // jank-free to ~200 KB on mid hardware; chosen at half that). Rejected
  // alternative: silently truncating the paste — that would rehearse a
  // document the real fetch never returns, so leaves could resolve against
  // phantom data.
  const SAMPLE_MAX_BYTES = 128 * 1024;

  interface TreeNode {
    label: string;
    segs: string[];
    /** null = leaf (primitive). */
    children: TreeNode[] | null;
    preview: string;
  }

  let {
    name,
    mode = 'token',
    onInsert,
    onPickPath,
    disabled = false
  }: {
    name: string;
    /** token: leaf click inserts `{urlfetch:name.path}` via onInsert (palette
     * use). defpath: leaf click sets the definition's own json_path via
     * onPickPath (builder field use). */
    mode?: 'token' | 'defpath';
    onInsert: (token: string) => void;
    onPickPath?: (segments: string[]) => void;
    disabled?: boolean;
  } = $props();

  let sample = $state('');
  let error = $state('');
  let tree = $state<TreeNode[]>([]);
  let open = $state(false);

  const encoder = new TextEncoder();

  function toggle() {
    open = !open;
    if (!open) {
      error = '';
    }
  }

  function truncate(s: string, n: number): string {
    return s.length > n ? s.slice(0, n - 1) + '…' : s;
  }

  // Build the whole visible tree up front; node count is bounded by the
  // NODE_CAP below so a hostile 128KB of nested arrays cannot mint a
  // page-freezing DOM even though parsing succeeds.
  const NODE_CAP = 600;
  let nodeBudget = NODE_CAP;

  function buildNode(label: string, value: unknown, segs: string[]): TreeNode | null {
    if (nodeBudget-- <= 0) return null;
    if (value !== null && typeof value === 'object') {
      const entries: [string, unknown][] = Array.isArray(value)
        ? value.map((v, i) => [String(i), v] as [string, unknown])
        : Object.entries(value);
      const children: TreeNode[] = [];
      for (const [k, v] of entries) {
        const child = buildNode(k, v, [...segs, k]);
        if (!child) break;
        children.push(child);
      }
      return { label, segs, children, preview: '' };
    }
    return { label, segs, children: null, preview: truncate(JSON.stringify(value) ?? '', 48) };
  }

  function onSample(v: string) {
    sample = v;
    error = '';
    tree = [];
    if (sample.trim() === '') return;
    if (encoder.encode(sample).length > SAMPLE_MAX_BYTES) {
      error = t('fetches.pickerTooBig', { max: String(Math.round(SAMPLE_MAX_BYTES / 1024)) });
      return;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(sample);
    } catch {
      error = t('fetches.pickerBadJson');
      return;
    }
    nodeBudget = NODE_CAP;
    const root = buildNode('', parsed, []);
    tree = root ? [root] : [];
    if (tree.length === 0) error = t('fetches.pickerHuge');
  }

  // A path is insertable only when it survives the shared grammar AND fits the
  // def's own depth budget — what the save validator would say later, said
  // earlier.
  function canPick(segs: string[]): boolean {
    return segs.length <= JSON_PATH_MAX_DEPTH && parseJsonPath(buildJsonPath(segs)) !== null;
  }

  function pick(segs: string[]) {
    if (!canPick(segs)) return;
    if (mode === 'defpath') {
      onPickPath?.(segs);
      open = false;
      return;
    }
    const slug = slugifyName(name);
    if (slug === '') return; // no name yet: the token would never resolve
    const dotted = buildJsonPath(segs);
    onInsert(dotted === '' ? `{urlfetch:${slug}}` : `{urlfetch:${slug}.${dotted}}`);
  }
</script>

<div class="fpp">
  <button
    type="button"
    class="var"
    title={t('fetches.pickerTitle')}
    aria-expanded={open}
    onclick={toggle}
    disabled={disabled}
  >{'{urlfetch:…path}'}</button>

  {#if open}
    <div class="panel" role="dialog" aria-label={t('fetches.pickerTitle')}>
      <p class="hint">{t('fetches.pickerHint')}</p>
      <textarea
        class="sample"
        rows="4"
        spellcheck="false"
        placeholder={t('fetches.pickerPlaceholder')}
        aria-label={t('fetches.pickerSampleAria')}
        bind:value={sample}
        oninput={(e) => onSample(e.currentTarget.value)}
      ></textarea>
      {#if error}
        <small class="err" role="alert">{error}</small>
      {:else if tree.length === 0}
        <p class="mut">{t('fetches.pickerEmpty')}</p>
      {/if}
      {#if tree.length > 0 && mode === 'token' && slugifyName(name) === ''}
        <small class="err" role="alert">{t('fetches.pickerNeedsName')}</small>
      {/if}
      {#if tree.length > 0}
        <div class="tree" data-lenis-prevent>
          {#each tree as root, i (i)}
            {@render node(root)}
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<!-- Recursive renderer: objects/arrays render their children indented; every
     primitive is a button whose click inserts the full token. -->
{#snippet node(n: TreeNode)}
  {#if n.children === null}
    <button
      type="button"
      class="leaf"
      disabled={!canPick(n.segs)}
      title={canPick(n.segs)
        ? `{urlfetch:${name}${n.segs.length ? '.' + buildJsonPath(n.segs) : ''}}`
        : t('fetches.pickerTooDeep')}
      onclick={() => pick(n.segs)}
    >
      <span class="leaf-key">{n.label === '' ? '/' : n.label}</span>
      <span class="leaf-val">{n.preview}</span>
    </button>
  {:else}
    <div class="branch">
      <span class="key">{n.label === '' ? t('fetches.pickerRoot') : n.label}</span>
      <div class="kids">
        {#each n.children as child, i (child.segs.join('.'))}
          {@render node(child)}
        {/each}
      </div>
    </div>
  {/if}
{/snippet}

<style>
  .fpp { position: relative; display: inline-flex; }

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
  .var:hover:not(:disabled) { background: rgba(201, 168, 124, 0.18); color: var(--bb-white); }
  .var:disabled { opacity: 0.45; cursor: default; }

  .panel {
    position: absolute;
    z-index: 30;
    top: calc(100% + 6px);
    left: 0;
    width: 320px;
    max-height: 380px;
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

  .hint, .mut {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    line-height: 1.5;
    color: var(--bb-muted);
  }
  .mut { font-style: italic; }

  .sample {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 64px;
    padding: 9px 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: 8px;
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    line-height: 1.5;
  }
  .sample:focus { outline: none; border-color: rgba(82, 183, 136, 0.5); }

  .err { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-status-error, #cf8a78); }

  .tree {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 220px;
    overflow-y: auto;
    border-top: 1px solid var(--rule, var(--bb-border));
    padding-top: 8px;
  }

  .key {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-tan-light);
  }
  .branch .kids { margin-left: 10px; padding-left: 8px; border-left: 1px solid var(--rule, var(--bb-border)); display: flex; flex-direction: column; gap: 2px; }

  .leaf {
    width: 100%;
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    padding: 3px 6px;
    background: transparent;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    text-align: left;
  }
  .leaf:hover:not(:disabled) { background: var(--glass-fill-2); }
  .leaf:disabled { cursor: default; opacity: 0.4; }
  .leaf-key { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-white); white-space: nowrap; }
  .leaf-val {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
