<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { FieldError, JSON_PATH_MAX_DEPTH, PickerOption, Text, buildJsonPath, getI18n, parseJsonPath } from '@bagel/kit';

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
  <FieldError message={parsed.error} />
{:else if parsed.tree.length > 0}
  <div class="tree">
    {#each parsed.tree as root (root.segs.join('.'))}
      {@render node(root)}
    {/each}
  </div>
{/if}

{#snippet node(n: TreeNode)}
  {#if n.children === null}
    <PickerOption
      label={n.label === '' ? '/' : n.label}
      description={n.preview}
      disabled={!canPick(n.segs)}
      title={canPick(n.segs) ? (leafTitle?.(n.segs) ?? buildJsonPath(n.segs)) : t('fetches.pickerTooDeep')}
      onclick={() => onPick(n.segs)}
    />
  {:else}
    <div class="branch">
      <Text as="span" size="xs" mono tone="accent">{n.label === '' ? t('fetches.pickerRoot') : n.label}</Text>
      <div class="kids">
        {#each n.children as child (child.segs.join('.'))}
          {@render node(child)}
        {/each}
      </div>
    </div>
  {/if}
{/snippet}

<style>
  .tree {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 260px;
    overflow-y: auto;
    padding-top: 8px;
    border-top: 1px solid var(--bb-border);
  }

  .branch .kids {
    margin-left: 10px;
    padding-left: 8px;
    border-left: 1px solid var(--bb-border);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
</style>
