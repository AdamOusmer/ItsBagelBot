<script lang="ts">
  let { title = 'ItsBagelBot', sub }: { title?: string; sub: string } = $props();
</script>

<div class="brand">
  <img src="/logo.png" alt={title} />
  <div>
    <div class="name">{title}</div>
    <div class="sub">{sub}</div>
  </div>
</div>

<style>
  .brand { display: flex; align-items: center; gap: 11px; padding: 6px 10px 22px; }
  .brand img { width: 30px; height: 30px; border-radius: 8px; }
  .name { font-family: var(--bb-font-display); font-weight: 700; font-size: 16px; letter-spacing: -0.01em; color: var(--bb-white); line-height: 1; }
  .sub { font-family: var(--bb-font-mono); font-size: 9.5px; letter-spacing: 0.16em; text-transform: uppercase; color: var(--bb-muted); margin-top: 4px; }
</style>
