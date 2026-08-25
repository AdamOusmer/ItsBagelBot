<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Clickable JSON tree over one response document. Given raw JSON text it
  // renders a browsable tree and calls onPick with the dotted segments of
  // whichever leaf the author clicks.
  //
  // Extracted from the old FetchPathPicker so the data-source builder and any
  // other caller share ONE implementation of the parse guards below. Two copies
  // drifting apart would mean a path that the tree offers but the save
  // validator rejects, which is the worst possible failure here: the author
  // clicks a value and is told the value is invalid.
  //
  // The grammar enforced is the Go resolver's — segments [A-Za-z0-9_-]+ (array
  // indices arrive as bare digits), depth <= JSON_PATH_MAX_DEPTH — so a path
  // picked here always parses server-side.
  import { JSON_PATH_MAX_DEPTH, buildJsonPath, getI18n, parseJsonPath } from '@bagel/shared';

  const { t } = getI18n();

  let {
    json,
    onPick,
    leafTitle
  }: {
    /** Raw response text. Empty renders nothing. */
    json: string;
    onPick: (segments: string[]) => void;
    /** Optional tooltip builder for a pickable leaf. */
    leafTitle?: (segments: string[]) => string;
  } = $props();

  // Pasted or fetched responses are capped well above any real single-value
  // payload: gossip refuses upstream bodies past 1 MiB post-decompression, and
  // typical JSON APIs answer in tens of KB. 128 KB keeps JSON.parse plus the
  // rendered tree inside the panel's interaction budget (measured jank-free to
  // ~200 KB on mid hardware; chosen at half that).
  //
  // Deliberately equal to maxSampleBytes in gossip's custom provider — the
  // server refuses to send more than this, so a smaller number here would
  // reject bodies it was willing to produce.
  //
  // Rejected alternative: silently truncating — that would rehearse a document
  // the real fetch never returns, so leaves could resolve against phantom data.
  const SAMPLE_MAX_BYTES = 128 * 1024;

  // Node count is bounded so a hostile 128KB of nested arrays cannot mint a
  // page-freezing DOM even though parsing succeeds.
  const NODE_CAP = 600;

  interface TreeNode {
    label: string;
    segs: string[];
    /** null = leaf (primitive). */
    children: TreeNode[] | null;
    preview: string;
  }

  const encoder = new TextEncoder();

  function truncate(s: string, n: number): string {
    return s.length > n ? s.slice(0, n - 1) + '…' : s;
  }

  function buildNode(label: string, value: unknown, segs: string[], budget: { left: number }): TreeNode | null {
    if (budget.left-- <= 0) return null;
    if (value !== null && typeof value === 'object') {
      const entries: [string, unknown][] = Array.isArray(value)
        ? value.map((v, i) => [String(i), v] as [string, unknown])
        : Object.entries(value);
      const children: TreeNode[] = [];
      for (const [k, v] of entries) {
        const child = buildNode(k, v, [...segs, k], budget);
        if (!child) break;
        children.push(child);
      }
      return { label, segs, children, preview: '' };
    }
    return { label, segs, children: null, preview: truncate(JSON.stringify(value) ?? '', 48) };
  }

  // Parsing is derived, not an event handler, so the tree tracks `json`
  // regardless of whether it arrived from a paste or from a server sample.
  const parsed = $derived.by<{ tree: TreeNode[]; error: string }>(() => {
    if (json.trim() === '') return { tree: [], error: '' };
    if (encoder.encode(json).length > SAMPLE_MAX_BYTES) {
      return { tree: [], error: t('fetches.pickerTooBig', { max: String(Math.round(SAMPLE_MAX_BYTES / 1024)) }) };
    }
    let doc: unknown;
    try {
      doc = JSON.parse(json);
    } catch {
      return { tree: [], error: t('fetches.pickerBadJson') };
    }
    const root = buildNode('', doc, [], { left: NODE_CAP });
    if (!root) return { tree: [], error: t('fetches.pickerHuge') };
    return { tree: [root], error: '' };
  });

  // A path is pickable only when it survives the shared grammar AND fits the
  // depth budget — what the save validator would say later, said earlier.
  function canPick(segs: string[]): boolean {
    return segs.length <= JSON_PATH_MAX_DEPTH && parseJsonPath(buildJsonPath(segs)) !== null;
  }
</script>

{#if parsed.error}
  <small class="err" role="alert">{parsed.error}</small>
{:else if parsed.tree.length > 0}
  <div class="tree" data-lenis-prevent>
    {#each parsed.tree as root (root.segs.join('.'))}
      {@render node(root)}
    {/each}
  </div>
{/if}

<!-- Recursive renderer: objects/arrays render their children indented; every
     primitive is a button whose click reports its path. -->
{#snippet node(n: TreeNode)}
  {#if n.children === null}
    <button
      type="button"
      class="leaf"
      disabled={!canPick(n.segs)}
      title={canPick(n.segs) ? (leafTitle?.(n.segs) ?? buildJsonPath(n.segs)) : t('fetches.pickerTooDeep')}
      onclick={() => onPick(n.segs)}
    >
      <span class="leaf-key">{n.label === '' ? '/' : n.label}</span>
      <span class="leaf-val">{n.preview}</span>
    </button>
  {:else}
    <div class="branch">
      <span class="key">{n.label === '' ? t('fetches.pickerRoot') : n.label}</span>
      <div class="kids">
        {#each n.children as child (child.segs.join('.'))}
          {@render node(child)}
        {/each}
      </div>
    </div>
  {/if}
{/snippet}

<style>
  .err { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-status-error, #cf8a78); }

  .tree {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 260px;
    overflow-y: auto;
    padding-top: 8px;
    border-top: 1px solid var(--rule, var(--bb-border));
  }

  .key { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-tan-light); }
  .branch .kids {
    margin-left: 10px;
    padding-left: 8px;
    border-left: 1px solid var(--rule, var(--bb-border));
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .leaf {
    width: 100%;
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    padding: 4px 6px;
    background: transparent;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    text-align: left;
  }
  .leaf:hover:not(:disabled) { background: var(--glass-fill-2); }
  .leaf:focus-visible { outline: 2px solid var(--bb-green-glow, #52b788); outline-offset: -2px; }
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
