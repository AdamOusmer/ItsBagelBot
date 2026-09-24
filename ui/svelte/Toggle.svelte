<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import Switch from './Switch.svelte';

  let {
    on = $bindable(false),
    onchange,
    label = 'Toggle',
    disabled = false,
    pending = false
  }: {
    on?: boolean;
    onchange?: (v: boolean) => void;
    label?: string;
    disabled?: boolean;
    pending?: boolean;
  } = $props();
</script>

<Switch bind:checked={on} {onchange} {label} {disabled} {pending} />
