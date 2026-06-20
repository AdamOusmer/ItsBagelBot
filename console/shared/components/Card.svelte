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
  .card { background: var(--glass-fill); border: 1px solid var(--glass-border); border-radius: var(--bb-radius-lg);
    backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat)) brightness(var(--glass-bright)); -webkit-backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat)) brightness(var(--glass-bright));
    box-shadow: var(--glass-rim), var(--glass-shadow);
    padding: var(--card-pad); position: relative; overflow: hidden;
    transition: border-color var(--bb-dur-base) var(--bb-ease-out-expo), transform var(--bb-dur-base) var(--bb-ease-out-expo); }
  .card::after {
    content: ""; position: absolute; inset: 0; pointer-events: none; z-index: 0;
    border-radius: inherit; padding: 1px; background: var(--glass-diffraction);
    -webkit-mask: linear-gradient(#000 0 0) content-box, linear-gradient(#000 0 0);
    mask: linear-gradient(#000 0 0) content-box, linear-gradient(#000 0 0);
    -webkit-mask-composite: xor; mask-composite: exclude; opacity: 0.9; }
  .card.sheen::before { content: ""; position: absolute; inset: 0; pointer-events: none;
    background: radial-gradient(circle at 92% 6%, rgba(82,183,136,0.10), transparent 46%); }
  .card.stat { padding: calc(22px * var(--d)); }
</style>
