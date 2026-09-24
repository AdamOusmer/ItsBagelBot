<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { AlertBanner, Button, Card, ConfirmDialog, SaveStatus, getI18n } from '@bagel/kit';
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
  <AlertBanner variant="warn">
    {t('discord.conflictBody')}
    {#snippet action()}
      <Button variant="secondary" onclick={draft.reload}>{t('discord.conflictCta')}</Button>
    {/snippet}
  </AlertBanner>
{/if}

{#if draft.invalidBanner}
  <AlertBanner variant="warn">{draft.invalidBanner}</AlertBanner>
{/if}

<section class="block reveal" style="--i:{index}" aria-labelledby={id}>
  <h2 {id} class="block-title">{title}</h2>
  <Card>
    <form method="POST" action="?/save" use:enhance={draft.saveSubmit} novalidate>
      <input type="hidden" name="config" value={draft.payload} />
      <input type="hidden" name="version" value={draft.version} />
      {#if hint}<p class="hint">{hint}</p>{/if}

      {@render children()}

      <div class="actions">
        <SaveStatus state={draft.saveState} />
        <Button variant="primary" type="submit" loading={draft.saving}>{t('discord.save')}</Button>
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
  danger
  onConfirm={draft.confirmDiscard}
  onCancel={draft.cancelDiscard}
/>
