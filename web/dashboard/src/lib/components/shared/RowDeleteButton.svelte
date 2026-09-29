<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Icon, IconButton } from '@bagel/ui/svelte';

  let {
    label,
    onclick,
    disabled = false
  }: {
    label: string;
    onclick: () => void;
    disabled?: boolean;
  } = $props();
</script>

<span class="row-delete">
  <IconButton size="sm" {label} {disabled} {onclick} tone="danger"><Icon name="trash" size={15} /></IconButton>
</span>

<style>
  .row-delete { display: inline-flex; position: relative; --btn-icon-min-size: 32px; }
  .row-delete::after {
    content: '';
    position: absolute;
    inset: -6px;
    pointer-events: none;
  }
  @media (pointer: coarse), (max-width: 760px) {
    .row-delete { --btn-icon-min-size: 44px; }
    .row-delete::after { inset: 0; }
  }
</style>
