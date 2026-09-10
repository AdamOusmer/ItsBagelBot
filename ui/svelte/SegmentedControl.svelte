<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // In-page filter as one accessible control (radiogroup) on the shared tab
  // rail. Options are plain strings; the bound value is the selected one.
  //
  // Not a form field: this is JS-only chip state and posts nothing. The
  // control that survives a no-JS submit is RadioGroup, and the two are named
  // apart on purpose.
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

<div class="bb-seg bb-tabs" role="radiogroup" aria-label={label}>
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
