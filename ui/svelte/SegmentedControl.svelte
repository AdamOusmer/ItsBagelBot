<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Thin adapter over THE RAIL (`.bb-tabs`, ../styles/tags.css): an in-page
  // filter as one accessible control. Options are plain strings; the bound
  // value is the selected one.
  //
  // It used to carry `.bb-seg` (../styles/elements/segmented.css) on top of
  // the rail, and before that a row of boxed pills. Both are gone: the file
  // held six lines of small-screen overflow behaviour and nothing else, and
  // those are `.bb-tabs--wrap` now, so a filter and a section jump are
  // literally the same element again.
  //
  // Not a form field: this is JS-only state and posts nothing. The control
  // that survives a no-JS submit is RadioGroup -- which wears the same rail,
  // with real <input type="radio"> inside each tab -- and the two are named
  // apart on purpose.
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
