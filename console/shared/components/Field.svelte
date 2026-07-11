<script lang="ts">
  // Labeled form-field wrapper. The caller passes the actual control (e.g.
  // <input class="search">) as children, so the binding stays in the caller.
  // `tag` is an optional muted suffix like "default: 5".
  import type { Snippet } from 'svelte';
  let { label, tag, children }: { label: string; tag?: string; children: Snippet } = $props();
</script>

<label class="field">
  <span>{label}{#if tag} <small class="tag">{tag}</small>{/if}</span>
  {@render children()}
</label>

<style>
  .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
  .field > span { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .tag { opacity: 0.65; font-size: 11px; }
  .field :global(.search) { width: 100%; box-sizing: border-box; }
</style>
