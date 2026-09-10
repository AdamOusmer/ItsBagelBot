<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // DEPRECATED. Thin bindable wrapper over Switch, kept only for the existing
  // `bind:on` call sites; it exists for a prop NAME, not for behaviour. New
  // code uses Switch directly, and it has no Astro twin on purpose -- a static
  // adapter for an alias would make the alias look like part of the contract.
  // `label` defaults for back-compat but callers should pass a real name.
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
