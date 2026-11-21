<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SubmitFunction } from '$app/forms';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { statusTone } from '@bagel/kit/status-tone';
  import type { AccessKey } from '#lib/access';
  import type { GiveawayWinnerWire } from '#lib/server/giveaways';
  import type { AdminUserWire, AuditEntry, ChannelSubState } from '#lib/server/services';
  import StatePill from '../StatePill.svelte';
  import ChannelLink from './ChannelLink.svelte';
  import UserAccount from './UserAccount.svelte';
  import UserConnection from './UserConnection.svelte';
  import UserHistory from './UserHistory.svelte';
  import UserInspectorActions from './UserInspectorActions.svelte';
  import UserSupport from './UserSupport.svelte';
  import UserTier from './UserTier.svelte';
  import { actionsFor, type UserActionDef } from './user-actions';
  import { userDates } from './user-dates';
  import { stateOf, USER_BADGE_LABEL } from './user-state';

  let {
    user,
    tokenPresent,
    subState,
    viewAsUrl,
    history,
    historyError,
    prizeHistory,
    prizeHistoryError,
    canReadHistory,
    busy,
    can,
    onAction,
    onStatus,
    onMessage,
    creatorSubmit
  }: {
    user: AdminUserWire;
    tokenPresent: boolean | null;
    subState: ChannelSubState | null;
    viewAsUrl: string;
    history: AuditEntry[] | null;
    historyError: string;
    prizeHistory: GiveawayWinnerWire[] | null;
    prizeHistoryError: string;
    canReadHistory: boolean;
    busy: string | null;
    can: (key: AccessKey) => boolean;
    onAction: (def: UserActionDef) => void;
    onStatus: (status: string) => void;
    onMessage: () => void;
    creatorSubmit: SubmitFunction;
  } = $props();

  const { locale, t } = getI18n();
  const { ago, date } = userDates(locale);
  const state = $derived(stateOf(user));
  const serviceActions = $derived(actionsFor('service', user, can));
  const supportActions = $derived(actionsFor('support', user, can));
  const dangerActions = $derived(actionsFor('danger', user, can));
  const canMessage = $derived(can('notifications.send'));
  const auditRows = $derived(history?.map((entry) => ({
    id: entry.id,
    label: `${entry.action} · @${entry.actor_login}`,
    when: ago(entry.created_at),
    outcome: t(entry.ok ? 'admin.audit.outcomeOk' : 'admin.audit.outcomeFailed'),
    tone: statusTone(entry.ok ? 'online' : 'degraded')
  })) ?? null);
  const prizeRows = $derived(prizeHistory?.map((award) => ({
    id: award.id,
    label: `${t('admin.users.prizeMonths', { n: award.prizeMonths })} · ${award.awardState}`,
    when: award.startAt ? date(award.startAt) : t('admin.giveaways.noDate'),
    tone: statusTone(award.awardState === 'completed' ? 'online' : 'degraded')
  })) ?? null);
</script>

<Scroller fill padding="18px" smooth>
  <Stack gap={5} style="max-inline-size: 37rem; margin-inline: auto">
    <div class="identity">
      <Bolota name={user.username} size={44} active gate />
      <div class="identity-name">
        <Heading level={2} variant="title" title={user.username}>
          <bb-fit><span data-fit>{user.username}</span></bb-fit>
        </Heading>
      </div>
      <div class="state">
        <StatePill tone={state}>
          <span class="state-label"><bb-fit><span data-fit>{t(USER_BADGE_LABEL[state])}</span></bb-fit></span>
        </StatePill>
      </div>
      <ChannelLink login={user.username} label={t('admin.users.channelLink', { login: user.username })} />
      <div class="identity-meta">
        <Text size="xs" tone="muted">
          <bb-fit><span data-fit>{t('admin.users.identityMeta', {
            id: String(user.id),
            joined: date(user.created_at),
            updated: ago(user.updated_at)
          })}</span></bb-fit>
        </Text>
      </div>
    </div>

    <FactList>
      <Fact term={t('admin.users.termsLabel')} fit>
        {user.onboarded ? t('admin.users.termsAccepted') : t('admin.users.termsPending')}
      </Fact>
    </FactList>

    <UserTier {user} {onStatus} {date} disabled={busy !== null || !can('users.grant')} />

    <UserConnection {user} {tokenPresent} {subState} {ago}>
      {#if serviceActions.length}
        <UserInspectorActions actions={serviceActions} {user} {busy} {onAction} />
      {/if}
    </UserConnection>

    {#key user.id}
      <UserAccount {user} {busy} {can} {creatorSubmit} />
    {/key}

    {#if supportActions.length || canMessage}
      {#key user.id}
        <UserSupport {viewAsUrl} showLink={can('users.impersonate')}>
          <UserInspectorActions actions={supportActions} {user} {busy} {onAction}>
            {#if canMessage}
              <Button variant="ghost" disabled={busy !== null} onclick={onMessage} block>
                <bb-fit><span data-fit>{t('admin.users.messageCta')}</span></bb-fit>
              </Button>
            {/if}
          </UserInspectorActions>
        </UserSupport>
      {/key}
    {/if}

    {#if canReadHistory}
      <UserHistory
        title={t('admin.users.historyTitle')}
        loading={t('admin.users.historyLoading')}
        empty={t('admin.users.historyEmpty')}
        rows={auditRows}
        error={historyError}
        more={{ href: `/audit?q=${user.id}`, label: t('admin.users.historyMore') }}
      />
    {/if}

    {#if can('giveaways.manage')}
      <UserHistory
        title={t('admin.users.prizeHistoryTitle')}
        loading={t('admin.users.prizeHistoryLoading')}
        empty={t('admin.users.prizeHistoryEmpty')}
        rows={prizeRows}
        error={prizeHistoryError}
      />
    {/if}

    {#if dangerActions.length}
      <Stack as="section" gap={2}>
        <Heading level={3} variant="label">{t('admin.users.dangerTitle')}</Heading>
        <UserInspectorActions actions={dangerActions} {user} {busy} {onAction} />
      </Stack>
    {/if}
  </Stack>
</Scroller>

<style>
  .identity {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) 5.5rem auto;
    gap: var(--bb-space-2);
    align-items: center;
  }

  .identity-name,
  .identity-meta {
    overflow: hidden;
  }

  .identity-meta {
    grid-column: 1 / -1;
  }

  .state {
    justify-self: end;
    max-inline-size: 100%;
    min-inline-size: 0;
  }

  .state-label {
    inline-size: 5.25rem;
    text-align: center;
  }
</style>
