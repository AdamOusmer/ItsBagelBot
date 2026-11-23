<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { usageTone, VOICE_TOKENS_PER_SECOND } from '@bagel/kit/voice-plan';
  import type { AdminVoiceAllowance } from '#lib/server/services';

  let { allowance, children }: { allowance: AdminVoiceAllowance; children: Snippet } = $props();
  const { t, locale } = getI18n();
  const minutes = new Intl.NumberFormat(locale, { maximumFractionDigits: 2 });
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });
  const tokensPerMinute = VOICE_TOKENS_PER_SECOND * 60;
  const usage = $derived(t('admin.users.voice.usage', {
    used: minutes.format(allowance.used_tokens / tokensPerMinute),
    limit: minutes.format(allowance.limit_tokens / tokensPerMinute)
  }));
  const resetDate = $derived(dates.format(new Date(allowance.resets_at)));
</script>

<Stack as="section" gap={2} aria-label={t('admin.users.voice.title')}>
  <Heading level={3} variant="label">{t('admin.users.voice.title')}</Heading>
  <Text size="sm"><bb-fit><span data-fit>{usage}</span></bb-fit></Text>
  <ProgressBar
    inset
    value={allowance.used_tokens / allowance.limit_tokens}
    tone={usageTone(allowance.used_tokens, allowance.limit_tokens)}
    label={t('admin.users.voice.title')}
    aria-valuetext={usage}
  />
  <Text size="xs" tone="muted">
    <bb-fit><span data-fit>{t('admin.users.voice.resets', { date: resetDate })}</span></bb-fit>
  </Text>
  {@render children()}
</Stack>
