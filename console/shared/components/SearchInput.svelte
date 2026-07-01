<script lang="ts">
  // Search field with the standard icon, an optional debounce for callers that
  // trigger work per keystroke, and a one-tap clear.
  import Icon from './Icon.svelte';

  let {
    value = $bindable(''),
    placeholder = 'Search…',
    debounceMs = 0,
    oninput = undefined as ((value: string) => void) | undefined
  } = $props();

  let timer: ReturnType<typeof setTimeout> | undefined;

  function changed() {
    if (!oninput) return;
    if (!debounceMs) {
      oninput(value);
      return;
    }
    clearTimeout(timer);
    timer = setTimeout(() => oninput?.(value), debounceMs);
  }

  function clear() {
    value = '';
    clearTimeout(timer);
    oninput?.('');
  }
</script>

<label class="search si">
  <Icon name="search" size={15} />
  <input type="text" {placeholder} bind:value oninput={changed} />
  {#if value}
    <button type="button" class="clear" aria-label="Clear search" onclick={clear}>
      <Icon name="x" size={12} />
    </button>
  {/if}
</label>

<style>
  .si { position: relative; display: inline-flex; align-items: center; }
  .si input { flex: 1; min-width: 0; }
  .clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    flex: none;
    border: none;
    background: transparent;
    color: var(--bb-muted);
    cursor: pointer;
    border-radius: 6px;
  }
  .clear:hover { color: var(--bb-white); }
</style>
