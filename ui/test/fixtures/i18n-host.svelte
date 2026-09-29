<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { setUiI18n } from '../../svelte/i18n';
  import type { UiOverride } from '../../lib/i18n';
  import SaveStatus from '../../svelte/SaveStatus.svelte';
  import EditorFooter from '../../svelte/EditorFooter.svelte';

  let { locale, override }: { locale: string; override?: UiOverride } = $props();
  // svelte-ignore state_referenced_locally
  setUiI18n(() => locale, override);
</script>

<SaveStatus state="saving" /><SaveStatus state="live" /><EditorFooter dirty onCancel={() => {}} />
