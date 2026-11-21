<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { ComponentProps } from 'svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';

  type Props = Omit<ComponentProps<typeof SegmentedControl>, 'class' | 'even'> & {
    label: string;
    countWidth?: string;
    disabled?: boolean;
  };

  let { value = $bindable(''), countWidth, disabled, ...tabs }: Props = $props();
</script>

<fieldset class="filter-tabs" {disabled} style:--tab-count-w={countWidth}>
  <SegmentedControl {...tabs} class="bb-tabs--scroll" bind:value />
</fieldset>

<style>
  .filter-tabs {
    inline-size: 100%;
    min-inline-size: 0;
    margin: 0;
    padding: 0;
    border: 0;
  }
</style>
