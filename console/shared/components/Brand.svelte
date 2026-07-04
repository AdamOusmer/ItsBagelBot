<script lang="ts">
  let { title = 'ItsBagelBot', sub }: { title?: string; sub: string } = $props();
</script>

<!-- Call-sign block: mark + station id, ruled off from the nav below. -->
<div class="brand">
  <img src="/logo.png" alt={title} />
  <div>
    <div class="name">{title}</div>
    <div class="sub">{sub}</div>
  </div>
</div>

<style>
  .brand { display: flex; align-items: center; gap: 11px; padding: 4px 6px 18px; }
  .brand img { width: 30px; height: 30px; border-radius: 8px; }
  .name { font-family: var(--bb-font-display); font-weight: 800; font-size: 15px; letter-spacing: -0.01em; color: var(--bb-white); line-height: 1; }
  .sub { font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.22em; text-transform: uppercase; color: var(--bb-tan); margin-top: 5px; }
</style>
