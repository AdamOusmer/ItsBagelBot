<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ChangelogDraft } from './ship';

  let { draft = $bindable() }: { draft: ChangelogDraft } = $props();

  const { t } = getI18n();
</script>

<Grid cols={2} gap={3} --field-mb="0px">
  <Stack gap={3}>
    <Field label={t('admin.deploys.changelogTitleEn')} for="cl-title-en" fit>
      <Input id="cl-title-en" fill bind:value={draft.titleEn} lang="en" required />
    </Field>
    <Field label={t('admin.deploys.changelogHighlightsEn')} for="cl-hl-en" fit>
      <Textarea id="cl-hl-en" fill rows={3} bind:value={draft.highlightsEn} lang="en" />
    </Field>
  </Stack>
  <Stack gap={3}>
    <Field label={t('admin.deploys.changelogTitleFr')} for="cl-title-fr" fit>
      <Input id="cl-title-fr" fill bind:value={draft.titleFr} lang="fr" />
    </Field>
    <Field label={t('admin.deploys.changelogHighlightsFr')} for="cl-hl-fr" fit>
      <Textarea id="cl-hl-fr" fill rows={3} bind:value={draft.highlightsFr} lang="fr" />
    </Field>
  </Stack>
</Grid>
