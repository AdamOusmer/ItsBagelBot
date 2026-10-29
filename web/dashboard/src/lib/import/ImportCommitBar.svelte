<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ActionBar from '@bagel/ui/svelte/ActionBar.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';

  let { line, submitting, cancelLabel, submitLabel, oncancel, oncommit }: {
    line: string;
    submitting: boolean;
    cancelLabel: string;
    submitLabel: string;
    oncancel: () => void;
    oncommit: () => void;
  } = $props();
</script>

<ActionBar
  as="form"
  roomy
  style="--action-bar-inset: 18px"
  onsubmit={(e: SubmitEvent) => {
    e.preventDefault();
    oncommit();
  }}
>
  <Label mono as="span">{line}</Label>
  {#snippet actions()}
    <Button variant="ghost" type="button" onclick={oncancel}>{cancelLabel}</Button>
    <Button type="submit" variant="primary" busy={submitting}>{submitLabel}</Button>
  {/snippet}
</ActionBar>
