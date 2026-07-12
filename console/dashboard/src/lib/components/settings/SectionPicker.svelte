<script lang="ts">
  // The permission group for a share link: a real <fieldset><legend> so the set
  // of section checkboxes is announced as one labelled group. Each CheckButton
  // carries its own name so the enclosing form submits `<section>=on`. When the
  // caller passes an `error`, the fieldset is marked invalid and points at the
  // message via aria-describedby.
  import CheckButton from '../CheckButton.svelte';
  import { FieldError } from '@bagel/shared';

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

<!-- No aria-invalid here: it is not a supported attribute on a <fieldset>'s
     implicit role="group". The error is associated via aria-describedby (which
     points at the id-bearing wrapper) and announced by FieldError's role="alert".
     The shared FieldError takes no id prop, so the wrapper carries it. -->
<fieldset
  class="section-picker"
  class:compact
  aria-describedby={invalid ? errorId : undefined}
>
  <legend>{legend}</legend>
  <div class="picks">
    {#each options as opt (opt.value)}
      <CheckButton name={opt.value} checked={opt.checked} label={opt.label} />
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
