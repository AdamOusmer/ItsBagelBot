<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One sub-page's settings block: the titled section, the save form, the two
  // banners a save can raise, and the unsaved-changes dialog.
  //
  // Every guild sub-page is the same frame around a different set of rows, so
  // the frame is a component. That is also what guarantees the two hidden
  // fields are always both present: `config` without `version` is a save that
  // cannot detect a conflict, and it would be invisible until two people edited
  // the same server at once.
  import { enhance } from '$app/forms';
  import { AlertBanner, Button, Card, ConfirmDialog, SaveStatus, getI18n } from '@bagel/shared';
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
    /** id of the section heading, wired to aria-labelledby. */
    id: string;
    title: string;
    hint?: string;
    /** Stagger position for the .reveal entrance. */
    index?: number;
    children: Snippet;
    /** Rendered inside the card, after the form (the ticket repost bar). */
    after?: Snippet;
  } = $props();

  const { t } = getI18n();
</script>

<!--
  A save refused because the row moved under us. Not retried and not merged: two
  drafts of the same server differ in ways only a human can reconcile, and the
  old module-blob save silently reverted whichever mod saved first.
-->
{#if draft.conflicted}
  <AlertBanner variant="warn" icon="ban">
    {t('discord.conflictBody')}
    {#snippet action()}
      <Button variant="secondary" icon="power" onclick={draft.reload}>{t('discord.conflictCta')}</Button>
    {/snippet}
  </AlertBanner>
{/if}

<!-- A save that landed with one field refused. The banner names the first one
     and each refused control carries its own note. Only this page's own slice
     is posted, so a refusal here is always about a control on this page. -->
{#if draft.invalidBanner}
  <AlertBanner variant="warn" icon="ban">{draft.invalidBanner}</AlertBanner>
{/if}

<section class="block reveal" style="--i:{index}" aria-labelledby={id}>
  <h2 {id} class="block-title">{title}</h2>
  <Card>
    <form method="POST" action="?/save" use:enhance={draft.saveSubmit} novalidate>
      <!-- The whole slice as one hidden JSON field. The old form posted a
           `name` per input plus a hidden mirror per switch, which meant the
           tri-state flags had three sources of truth and a control the page
           chose not to render silently cleared its field. One field means the
           page's own state object IS the payload, and every rule lives in the
           shared merge. -->
      <input type="hidden" name="config" value={draft.payload} />
      <input type="hidden" name="version" value={draft.version} />
      {#if hint}<p class="hint">{hint}</p>{/if}

      {@render children()}

      <div class="actions">
        <SaveStatus state={draft.saveState} />
        <Button variant="primary" type="submit" icon="check" loading={draft.saving}>{t('discord.save')}</Button>
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
