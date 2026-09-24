<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { JSON_PATH_MAX_DEPTH, buildJsonPath, getI18n, parseJsonPath } from '@bagel/kit';

  const { t } = getI18n();

  let {
    json,
    onPick,
    leafTitle
  }: {
    json: string;
    onPick: (segments: string[]) => void;
    leafTitle?: (segments: string[]) => string;
  } = $props();

  // Must equal gossip's maxSampleBytes.
  const SAMPLE_MAX_BYTES = 128 * 1024;

  const NODE_CAP = 600;

  interface TreeNode {
    label: string;
    segs: string[];
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

  function canPick(segs: string[]): boolean {
    return segs.length <= JSON_PATH_MAX_DEPTH && parseJsonPath(buildJsonPath(segs)) !== null;
  }
</script>

{#if parsed.error}
  <small class="err" role="alert">{parsed.error}</small>
{:else if parsed.tree.length > 0}
  <div class="tree">
    {#each parsed.tree as root (root.segs.join('.'))}
      {@render node(root)}
    {/each}
  </div>
{/if}

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
    border-radius: var(--bb-radius-sm);
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
