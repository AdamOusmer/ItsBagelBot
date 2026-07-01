<script lang="ts">
  import type { Snippet } from 'svelte';
  let {
    sheen = false,
    stat = false,
    rule = false,
    class: cls = '',
    children,
    ...rest
  }: { sheen?: boolean; stat?: boolean; rule?: boolean; class?: string; children: Snippet; [key: string]: unknown } = $props();
</script>

<div class="card {sheen ? 'sheen' : ''} {stat ? 'stat' : ''} {rule ? 'rule' : ''} {cls}" {...rest}>
  {@render children()}
</div>

<style>
  /* Ledger panel: flat ink fill, hairline frame, a stronger top rule like a
     section heading in a broadcast log. No blur, no glow, no diffraction. */
  .card {
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.028), rgba(240, 236, 228, 0.012));
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.11));
    border-top-color: var(--rule-strong, rgba(240, 236, 228, 0.22));
    border-radius: var(--bb-radius-lg);
    padding: var(--card-pad);
    position: relative;
  }
  .card.sheen::before { content: ""; position: absolute; inset: 0; pointer-events: none;
    background: radial-gradient(circle at 90% 0%, rgba(82, 183, 136, 0.07), transparent 45%); }
  .card.stat { padding: calc(20px * var(--d)); }
</style>
