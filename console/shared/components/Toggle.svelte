<script lang="ts">
  // Thin bindable wrapper over the shared Switch, kept for the existing
  // `bind:on` call sites. New code can use Switch directly. `label` defaults for
  // back-compat but callers should pass a real accessible name.
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
