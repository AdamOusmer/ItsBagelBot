<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/button.css';
  import Icon from '@bagel/ui/svelte/Icon.svelte';

  let {
    login,
    label
  }: {
    login: string;
    label: string;
  } = $props();

  const href = $derived(`https://twitch.tv/${login}`);
</script>

<a
  class="bb-btn bb-btn--icon bb-btn--sm channel-link"
  {href}
  target="_blank"
  rel="noopener noreferrer"
  aria-label={label}
  title={label}
  data-mark=""
>
  <Icon name="twitch" size={14} />
</a>

<style>
  .channel-link {
    flex: none;
  }
</style>
