<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Button, Icon } from '@bagel/kit';

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
  <Button variant="icon" size="sm" class="row-delete-btn" danger type="button" aria-label={label} {disabled} {onclick}>
    <Icon name="trash" size={15} />
  </Button>
</span>

<style>
  .row-delete { display: inline-flex; position: relative; }
  :global(.row-delete-btn) { width: 32px; height: 32px; min-height: 32px; }
  .row-delete::after {
    content: '';
    position: absolute;
    inset: -6px;
    pointer-events: none;
  }
  @media (pointer: coarse), (max-width: 760px) {
    :global(.row-delete-btn) { width: 44px; height: 44px; min-height: 44px; min-width: 44px; }
    .row-delete::after { inset: 0; }
  }
</style>
