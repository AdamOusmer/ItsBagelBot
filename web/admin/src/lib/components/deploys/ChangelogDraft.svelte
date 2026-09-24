<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ChangelogDraft } from './ship';

  let { draft = $bindable() }: { draft: ChangelogDraft } = $props();

  const { t } = getI18n();
</script>

<div class="draft">
  <div class="pair">
    <Field label={t('admin.deploys.changelogTitleEn')} for="cl-title-en">
      <Input id="cl-title-en" fill bind:value={draft.titleEn} />
    </Field>
    <Field label={t('admin.deploys.changelogTitleFr')} for="cl-title-fr">
      <Input id="cl-title-fr" fill bind:value={draft.titleFr} lang="fr" />
    </Field>
  </div>
  <div class="pair">
    <Field label={t('admin.deploys.changelogHighlightsEn')} for="cl-hl-en">
      <Textarea id="cl-hl-en" fill rows={7} bind:value={draft.highlightsEn} />
    </Field>
    <Field label={t('admin.deploys.changelogHighlightsFr')} for="cl-hl-fr">
      <Textarea id="cl-hl-fr" fill rows={7} bind:value={draft.highlightsFr} lang="fr" />
    </Field>
  </div>
</div>

<style>
  .draft {
    display: flex;
    flex-direction: column;
    gap: 12px;
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
