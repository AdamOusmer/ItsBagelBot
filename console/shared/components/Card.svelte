<script lang="ts">
  import type { Snippet } from 'svelte';
  let {
    sheen = false,
    stat = false,
    rule = false,
    class: cls = '',
    children
  }: { sheen?: boolean; stat?: boolean; rule?: boolean; class?: string; children: Snippet } = $props();
</script>

<div class="card {sheen ? 'sheen' : ''} {stat ? 'stat' : ''} {rule ? 'rule' : ''} {cls}">
  {@render children()}
</div>
