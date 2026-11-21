<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { copyFlash } from '@bagel/ui/lib/clipboard';

  let { viewAsUrl, showLink, children }: {
    viewAsUrl: string;
    showLink: boolean;
    children: Snippet;
  } = $props();

  const { t } = getI18n();
  const noteId = $props.id();
  let copied = $state(false);
  let copiedUrl = '';
  const notMinted = $derived(!viewAsUrl);

  function copyLink() {
    void copyFlash(viewAsUrl, (on) => (copied = on));
  }

  $effect(() => {
    if (viewAsUrl === copiedUrl) return;
    copiedUrl = viewAsUrl;
    if (viewAsUrl) copyLink();
  });
</script>

<Stack as="section" gap={2}>
  <Heading level={3} variant="label">{t(showLink ? 'admin.users.viewAsTitle' : 'admin.users.actionsTitle')}</Heading>
  {@render children()}
  {#if showLink}
    <div class="link-slot" class:unavailable={notMinted} inert={notMinted}>
      <div class="link">
        <Input
          fill mono
          readonly
          value={viewAsUrl}
          aria-label={t('admin.users.viewAsLinkLabel')}
          aria-describedby={noteId}
        />
        <Button variant="ghost" onclick={copyLink} block aria-label={t('common.copy')}>
          <bb-fit><span data-fit>{copied ? t('common.copied') : t('common.copy')}</span></bb-fit>
        </Button>
      </div>
      <div class="note">
        <Text id={noteId} size="xs" tone="muted" style="block-size: 100%">
          <bb-fit mode="block"><span data-fit>{t('admin.users.viewAsNote')}</span></bb-fit>
        </Text>
      </div>
    </div>
    <span class="bb-sr-only" role="status">{copied ? t('common.copied') : ''}</span>
  {/if}
</Stack>

<style>
  .note {
    block-size: 1.75rem;
  }

  .link-slot {
    display: grid;
    gap: var(--bb-space-2);
    transition: opacity var(--bb-dur-fast) ease;
  }

  .link {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 6.5rem;
    gap: var(--bb-space-2);
    align-items: stretch;
  }

  .unavailable {
    opacity: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .link-slot {
      transition: none;
    }
  }
</style>
