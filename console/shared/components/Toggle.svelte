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
