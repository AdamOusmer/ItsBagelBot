<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';

  let {
    login,
    label
  }: {
    login: string;
    label: string;
  } = $props();

  const href = $derived(`https://twitch.tv/${encodeURIComponent(login)}`);
</script>

<ButtonLink
  variant="ghost"
  size="sm"
  class="bb-btn--icon"
  {href}
  target="_blank"
  rel="noopener noreferrer"
  aria-label={label}
  title={label}
>
  <Icon name="twitch" size={14} />
</ButtonLink>
