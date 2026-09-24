<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/tags.css';

  let {
    name,
    options,
    value = $bindable(''),
    label = 'Options'
  }: {
    name: string;
    options: readonly { value: string; label: string }[];
    value: string;
    label?: string;
  } = $props();
</script>

<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label={label}>
  {#each options as opt (opt.value)}
    <label class="bb-tab {value === opt.value ? 'is-active' : ''}">
      <input
        class="bb-tab__input"
        type="radio"
        {name}
        value={opt.value}
        checked={value === opt.value}
        onchange={() => (value = opt.value)}
      />
      {opt.label}
    </label>
  {/each}
</div>
