<script lang="ts">
  // Filter chips as one accessible control (radiogroup). Options are plain
  // strings; the bound value is the selected one.
  let {
    options,
    value = $bindable(''),
    label = 'Filter'
  }: {
    options: readonly string[];
    value: string;
    label?: string;
  } = $props();
</script>

<div class="seg" role="radiogroup" aria-label={label}>
  {#each options as opt (opt)}
    <button
      type="button"
      class="chip {value === opt ? 'on' : ''}"
      role="radio"
      aria-checked={value === opt}
      onclick={() => (value = opt)}
    >
      {opt}
    </button>
  {/each}
</div>

<style>
  .seg {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  @media (max-width: 760px) {
    .seg {
      overflow-x: auto;
      -webkit-overflow-scrolling: touch;
      flex-wrap: nowrap;
      scrollbar-width: none;
    }
    .seg::-webkit-scrollbar { display: none; }
  }
</style>
