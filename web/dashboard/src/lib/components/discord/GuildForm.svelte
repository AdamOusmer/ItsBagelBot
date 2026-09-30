<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit';
  import type { Snippet } from 'svelte';
  import type { GuildDraft } from '$lib/discord/guild-draft.svelte';

  let {
    draft,
    id,
    title,
    hint = '',
    index = 1,
    children,
    after
  }: {
    draft: GuildDraft;
    id: string;
    title: string;
    hint?: string;
    index?: number;
    children: Snippet;
    after?: Snippet;
  } = $props();

  const { t } = getI18n();
</script>

{#if draft.conflicted}
  <AlertBanner tone="warning">
    {t('discord.conflictBody')}
    {#snippet actions()}
      <Button variant="secondary" onclick={draft.reload}>{t('discord.conflictCta')}</Button>
    {/snippet}
  </AlertBanner>
{/if}

{#if draft.invalidBanner}
  <AlertBanner tone="warning">{draft.invalidBanner}</AlertBanner>
{/if}

<section class="block reveal" style="--i:{index}" aria-labelledby={id}>
  <Heading level={6} as="h2" variant="title" {id} class="block-title">{title}</Heading>
  <Card>
    <form method="POST" action="?/save" use:enhance={draft.saveSubmit} novalidate>
      <input type="hidden" name="config" value={draft.payload} />
      <input type="hidden" name="version" value={draft.version} />
      {#if hint}<Text size="sm" tone="muted" class="hint">{hint}</Text>{/if}

      {@render children()}

      <div class="actions">
        <SaveStatus state={draft.saveState} />
        <Button variant="primary" type="submit" busy={draft.saving}>{t('discord.save')}</Button>
      </div>
    </form>

    {#if after}{@render after()}{/if}
  </Card>
</section>

<ConfirmDialog
  open={draft.discardOpen}
  title={t('discord.unsavedTitle')}
  body={t('discord.unsavedBody')}
  confirmLabel={t('discord.unsavedConfirm')}
  cancelLabel={t('common.cancel')}
  onConfirm={draft.confirmDiscard}
  onCancel={draft.cancelDiscard}
  tone="danger"
/>
