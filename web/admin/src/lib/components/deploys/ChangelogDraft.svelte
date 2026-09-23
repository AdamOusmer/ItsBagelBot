<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // The changelog a release commits, as four plain fields. English is seeded
  // from the commit subjects and is the only required part; French is left
  // empty on purpose, because a French line derived from an English subject
  // reads as a machine translation, and an absent French block falls back to
  // English on the site.
  //
  // The fields stay on screen, disabled, for kinds that write no changelog,
  // so switching kind never collapses the panel under the pointer.
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ChangelogDraft } from './ship';

  let {
    draft = $bindable(),
    disabled = false
  }: {
    draft: ChangelogDraft;
    disabled?: boolean;
  } = $props();

  const { t } = getI18n();
</script>

<div class="draft" class:off={disabled}>
  <div class="pair">
    <Field label={t('admin.deploys.changelogTitleEn')} for="cl-title-en">
      <Input id="cl-title-en" fill bind:value={draft.titleEn} {disabled} />
    </Field>
    <Field label={t('admin.deploys.changelogTitleFr')} for="cl-title-fr">
      <Input id="cl-title-fr" fill bind:value={draft.titleFr} {disabled} lang="fr" />
    </Field>
  </div>
  <div class="pair">
    <Field label={t('admin.deploys.changelogHighlightsEn')} for="cl-hl-en">
      <Textarea id="cl-hl-en" fill rows={7} bind:value={draft.highlightsEn} {disabled} />
    </Field>
    <Field label={t('admin.deploys.changelogHighlightsFr')} for="cl-hl-fr">
      <Textarea id="cl-hl-fr" fill rows={7} bind:value={draft.highlightsFr} {disabled} lang="fr" />
    </Field>
  </div>
</div>

<style>
  .draft {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .draft.off {
    opacity: 0.5;
  }
  .pair {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }
  @media (max-width: 760px) {
    .pair {
      grid-template-columns: 1fr;
    }
  }
</style>
