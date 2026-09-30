<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import FieldError from '@bagel/ui/svelte/FieldError.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';

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
  <Label as="legend">{legend}</Label>
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
  .picks {
    margin-top: 10px;
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
