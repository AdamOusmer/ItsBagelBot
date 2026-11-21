<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { statusTone, type StatusTone } from '@bagel/kit/status-tone';

  const TOKEN = {
    checking: { tone: statusTone('connecting'), label: 'admin.users.tokenChecking' },
    present: { tone: statusTone('online'), label: 'admin.users.tokenPresent' },
    absent: { tone: statusTone('auth_required'), label: 'admin.users.tokenAbsent' },
    unavailable: { tone: statusTone('degraded'), label: 'admin.users.tokenUnavailable' }
  } as const;

  export type TokenStatus = keyof typeof TOKEN;
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import '@bagel/ui/styles/elements/collapse.css';
  import type { AdminUserWire, ChannelSubState, SubVerification } from '#lib/server/services';

  let { user, tokenStatus, subState, ago, children }: {
    user: AdminUserWire;
    tokenStatus: TokenStatus;
    subState: ChannelSubState | null;
    ago: (value?: string | null) => string;
    children?: Snippet;
  } = $props();

  const { t } = getI18n();
  const heading = $props.id();
  const SERVICE = {
    banned: { tone: statusTone('reauth_required'), label: 'admin.users.connectionBanned' },
    active: { tone: statusTone('online'), label: 'admin.users.connectionServing' },
    inactive: { tone: statusTone('disabled'), label: 'admin.users.connectionInactive' }
  } as const;
  const SUB_TONE = {
    ok: 'online',
    pending: 'connecting',
    failing: 'degraded',
    revoked: 'reauth_required',
    chat_banned: 'bot_banned',
    unknown: 'sub_unknown'
  } as const;
  const serving = $derived(SERVICE[user.banned ? 'banned' : user.is_active ? 'active' : 'inactive']);
  const token = $derived(TOKEN[tokenStatus]);
  const subTone = $derived(statusTone(subState ? SUB_TONE[subState.state] : 'connecting'));
  const subLabel = $derived(subState
    ? t('admin.users.eventsubState', { state: subState.state })
    : t('admin.users.eventsubChecking'));
  const verification = $derived(subState?.verification);
  const check = $derived(checkSummary(verification));
  const checkedLabel = $derived(subState?.checkedAt ? t('admin.users.eventsubChecked', { when: ago(subState.checkedAt) }) : '');
  const diagnostic = $derived(
    [...new Set([subState?.error, verification?.error].filter(Boolean))].join(' · ')
  );
  let shownChecked = $state('');
  let shownDiagnostic = $state('');

  $effect(() => {
    if (checkedLabel) shownChecked = checkedLabel;
  });

  $effect(() => {
    if (diagnostic) shownDiagnostic = diagnostic;
  });

  function checkSummary(result?: SubVerification) {
    if (!result) return t('admin.users.liveCheckNone');
    if (result.throttled) return t('admin.users.liveCheckThrottled');
    if (result.error) return t('admin.users.liveCheckFailed');

    const parts = [t('admin.users.liveCheckCount', { present: result.present, expected: result.expected })];
    if (result.missing?.length) parts.push(`${t('admin.users.liveCheckMissing')} ${result.missing.join(', ')}`);
    if (result.repairing) parts.push(t('admin.users.liveCheckRepairing'));
    return parts.join(', ');
  }
</script>

{#snippet status(tone: StatusTone, label: string)}
  <Cluster gap={2} nowrap>
    <StatusDot {tone} />
    <div class="status-copy">
      <Text as="div" size="sm"><bb-fit><span data-fit>{label}</span></bb-fit></Text>
    </div>
  </Cluster>
{/snippet}

<Stack as="section" gap={2}>
  <Heading id={heading} level={3} variant="label">{t('admin.users.connectionTitle')}</Heading>
  <Stack gap={1} aria-live="polite" aria-busy={subState === null}>
    {@render status(serving.tone, t(serving.label))}
    {@render status(token.tone, t(token.label))}
    <div class="gapped">
      {@render status(subTone, subLabel)}
      <div class="bb-collapse" class:is-open={Boolean(checkedLabel)} aria-hidden={!checkedLabel} inert={!checkedLabel}>
        <div>
          <div class="status-copy">
            <Text as="div" size="xs" tone="muted">
              <bb-fit><span data-fit>{shownChecked}</span></bb-fit>
            </Text>
          </div>
        </div>
      </div>
    </div>
  </Stack>

  <div class="verification gapped">
    <FactList>
      <Fact term={t('admin.users.liveCheck')}>{check}</Fact>
    </FactList>
    <div class="bb-collapse" class:is-open={Boolean(diagnostic)} aria-hidden={!diagnostic} inert={!diagnostic}>
      <div>
        <div class="diagnostic">
          <AlertBanner tone="danger" flush>{shownDiagnostic}</AlertBanner>
        </div>
      </div>
    </div>
  </div>
  {@render children?.()}
</Stack>

<style>
  .status-copy {
    flex: 1;
    min-inline-size: 0;
    overflow: hidden;
  }

  .gapped {
    --bb-collapse-gap: var(--bb-space-2);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }

  .verification {
    overflow-wrap: anywhere;
  }

  .diagnostic {
    overflow-wrap: anywhere;
  }
</style>
