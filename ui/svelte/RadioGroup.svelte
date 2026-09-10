<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Thin adapter over THE RAIL (`.bb-tabs`, ../styles/tags.css), in its FORM
  // CONTROL shape: each tab is a <label> wrapping a real
  // <input type="radio">, so the value still posts through native form
  // submission and still works with JavaScript off.
  //
  // It was a row of pills with a round filled dot
  // (`.bb-radio`, ../styles/elements/radio-group.css, deleted with this).
  // That made three visual answers to one question inside the console -- pill
  // filters, pill radios, the underline rail -- and the rail is the answer we
  // kept; the argument is written out at the top of the rail contract.
  //
  // The input is positioned and zero-sized rather than `display: none`, which
  // is the whole reason this element and SegmentedControl are two elements and
  // not one with a flag: this one survives a no-JS submit, the other is
  // client-only state that posts nothing.
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
