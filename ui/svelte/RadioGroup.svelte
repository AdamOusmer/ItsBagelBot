<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Form field: styled pills backed by real <input type="radio">, so it still
  // posts via native form submission and progressive enhancement. Use this
  // wherever the value has to reach the server; use SegmentedControl for an
  // in-page filter that only ever lives in the client.
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

<div class="bb-radio-group" role="radiogroup" aria-label={label}>
  {#each options as opt (opt.value)}
    <label class="bb-radio" data-on={value === opt.value ? '' : undefined}>
      <input
        type="radio"
        {name}
        value={opt.value}
        checked={value === opt.value}
        onchange={() => (value = opt.value)}
      />
      <span class="bb-radio__dot" aria-hidden="true"></span>
      {opt.label}
    </label>
  {/each}
</div>
