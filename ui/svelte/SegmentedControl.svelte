<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/tags.css';

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

<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label={label}>
  {#each options as opt (opt)}
    <button
      type="button"
      class="bb-tab {value === opt ? 'is-active' : ''}"
      role="radio"
      aria-checked={value === opt}
      onclick={() => (value = opt)}
    >
      {opt}
    </button>
  {/each}
</div>
