<script lang="ts">
  let {
    on = $bindable(false),
    onchange
  }: { on?: boolean; onchange?: (v: boolean) => void } = $props();
  function flip() {
    on = !on;
    onchange?.(on);
  }
</script>

<button
  type="button"
  class="toggle {on ? 'on' : ''}"
  role="switch"
  aria-checked={on}
  aria-label="toggle"
  onclick={flip}
></button>

<style>
  .toggle { width: 38px; height: 22px; display: inline-block; border-radius: 999px; background: rgba(255,255,255,0.06); border: 1px solid var(--glass-border); position: relative; cursor: pointer; flex-shrink: 0;
    transition: all var(--bb-dur-base) var(--bb-ease-out-back); }
  .toggle::after { content: ""; position: absolute; top: 2px; left: 2px; width: 16px; height: 16px; border-radius: 50%; background: var(--bb-muted);
    transition: all var(--bb-dur-base) var(--bb-ease-out-back); }
  .toggle.on { background: var(--ui-accent-soft); border-color: rgba(82,183,136,0.4); }
  .toggle.on::after { left: 18px; background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow); }
</style>
