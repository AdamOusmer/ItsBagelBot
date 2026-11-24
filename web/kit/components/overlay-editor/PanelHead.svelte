<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Fit from './Fit.svelte';

  let { title, closeLabel, onclose }: { title: string; closeLabel: string; onclose: () => void } = $props();
</script>

<header class="head">
  <h2 class="title"><Fit>{title}</Fit></h2>
  <IconButton label={closeLabel} size="sm" tooltip onclick={onclose}><Icon name="x" size={14} /></IconButton>
</header>

<style>
  .head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 16px 12px 10px 16px;
  }
  .title {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
    color: var(--bb-white);
  }
</style>
