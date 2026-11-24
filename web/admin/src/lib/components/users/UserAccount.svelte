<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AccessKey } from '#lib/access';
  import type { AdminUserWire } from '#lib/server/services';

  let { user, busy, can, creatorSubmit }: {
    user: AdminUserWire;
    busy: string | null;
    can: (key: AccessKey) => boolean;
    creatorSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const locked = $derived(busy !== null);
  const canGrant = $derived(can('users.grant'));
</script>

{#snippet flagSection(title: string, hint: string, action: string, current: boolean | undefined, labelOn: string, labelOff: string)}
  <Stack as="section" gap={2}>
    <Heading level={3} variant="label">{title}</Heading>
    <Text size="xs" tone="muted">{hint}</Text>
    <form method="POST" action="?/{action}" use:enhance>
      <input type="hidden" name="user_id" value={user.id} />
      <input type="hidden" name="active" value={current ? 'false' : 'true'} />
      <Button variant="ghost" type="submit" disabled={locked} busy={busy === action}>
        <bb-fit><span data-fit>{current ? labelOn : labelOff}</span></bb-fit>
      </Button>
    </form>
  </Stack>
{/snippet}

{#if can('users.test')}
  {@render flagSection(t('admin.users.testAccountTitle'), t('admin.users.testAccountHint'), 'setTestAccount', user.test_account, t('admin.users.markOrdinary'), t('admin.users.markTest'))}
{/if}

{#if can('users.alpha')}
  {@render flagSection(t('admin.users.alphaTitle'), t('admin.users.alphaHint'), 'setAlpha', user.alpha, t('admin.users.unmarkAlpha'), t('admin.users.markAlpha'))}
{/if}

{#if canGrant}
  <Stack as="section" gap={2}>
    <Heading level={3} variant="label">{t('admin.users.creatorTitle')}</Heading>
    <form class="code-form" method="POST" action="?/setCreatorCode" use:enhance={creatorSubmit}>
      <input type="hidden" name="user_id" value={user.id} />
      <Input
        fill mono
        type="text"
        name="creator_code"
        maxlength={64}
        placeholder={t('admin.users.creatorPlaceholder')}
        aria-label={t('admin.users.creatorTitle')}
        value={user.creator_code ?? ''}
      />
      <Button variant="ghost" type="submit" busy={busy === 'creator'} disabled={locked} block>
        <bb-fit><span data-fit>{t('admin.users.creatorSave')}</span></bb-fit>
      </Button>
    </form>
  </Stack>
{/if}

<style>
  .code-form {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 7rem;
    gap: var(--bb-space-2);
    align-items: stretch;
  }
</style>
