<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Checkbox } from '@bagel/kit';
  import { FieldError } from '@bagel/kit';

  let {
    legend,
    options,
    error = undefined,
    errorId = 'section-picker-error',
    compact = false
  }: {
    legend: string;
    options: { value: string; label: string; checked: boolean }[];
    error?: string;
    errorId?: string;
    compact?: boolean;
  } = $props();

  const invalid = $derived(error != null && error !== '');
</script>

<fieldset
  class="section-picker"
  class:compact
  aria-describedby={invalid ? errorId : undefined}
>
  <legend>{legend}</legend>
  <div class="picks">
    {#each options as opt (opt.value)}
      <Checkbox name={opt.value} checked={opt.checked}>{opt.label}</Checkbox>
    {/each}
  </div>
  {#if invalid}<div id={errorId}><FieldError message={error} /></div>{/if}
</fieldset>

<style>
  .section-picker {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
  }
  .section-picker legend {
    padding: 0;
    margin-bottom: 10px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
  }
  .picks {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .section-picker.compact .picks {
    flex-direction: row;
    flex-wrap: wrap;
    gap: 8px 16px;
  }
</style>
