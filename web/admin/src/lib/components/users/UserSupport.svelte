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
  import '@bagel/ui/styles/elements/collapse.css';
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
    <div class="bb-collapse" style="--bb-collapse-gap: var(--bb-space-2)" class:is-open={!notMinted} aria-hidden={notMinted} inert={notMinted}>
      <div>
        <div class="reveal">
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
          <Text id={noteId} size="xs" tone="muted">{t('admin.users.viewAsNote')}</Text>
        </div>
      </div>
    </div>
    <span class="bb-sr-only" role="status">{copied ? t('common.copied') : ''}</span>
  {/if}
</Stack>

<style>
  .reveal {
    display: grid;
    gap: var(--bb-space-2);
  }

  .link {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 6.5rem;
    gap: var(--bb-space-2);
    align-items: stretch;
  }

</style>
