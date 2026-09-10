<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Runtime database credentials, one Card per service.
  //
  // No deck and no inspector here, unlike the other management pages: there are
  // four services and each verb needs its own typed confirmation, so a
  // master-detail split would put a click in front of a card that already fits
  // on screen whole. What DID change is that the three dialogs became one table
  // (SECRET_DIALOGS) instead of three parallel `pending.kind ===` branches
  // spread across the title, the CTA, the danger flag, the fields and the form
  // action -- five places that had to be edited in step to add a fourth verb.
  //
  // The action names (`rotate`, `set`, `revoke`) are the server's and are not
  // renamed -- the audit trail keys off them.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/kit/components/PageHead.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import ConfirmDialog from '@bagel/kit/components/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import { toast } from '@bagel/kit/toast';
  import { actionPayload, adminToastFailure, copyFlash } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows } from '$lib/access';
  import type { DbCredentialStatus } from '$lib/server/secrets';
  import StatePill from '$lib/components/StatePill.svelte';
  import ServiceCard from '$lib/components/secrets/ServiceCard.svelte';
  import { SECRET_DIALOGS, type SecretVerb } from '$lib/components/secrets/secret-dialogs';
  import type { SecretsBundle } from './+page.server';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const canManage = $derived(allows(data.role, 'secrets.manage'));

  // Streamed bundle -> local state; refreshed via invalidateAll after writes,
  // because only Doppler knows what the credential became.
  let bundle = $state<SecretsBundle | null>(null);
  $effect(() => {
    let alive = true;
    data.bundle.then((b: SecretsBundle) => {
      if (alive) bundle = b;
    });
    return () => {
      alive = false;
    };
  });

  const services = $derived(bundle?.services ?? []);

  // ── Dialog state ───────────────────────────────────────────────────────────
  let pendingVerb = $state<SecretVerb | null>(null);
  let pendingService = $state<DbCredentialStatus | null>(null);
  let confirmText = $state('');
  let dbUser = $state('');
  let dbPass = $state('');
  let busy = $state(false);
  let dialogForm = $state<HTMLFormElement | null>(null);

  const dialog = $derived(pendingVerb ? SECRET_DIALOGS[pendingVerb] : null);
  const phrase = $derived(dialog && pendingService ? dialog.phrase(pendingService, dbUser) : '');
  // The server checks this too, and its answer is the one that counts; matching
  // here only stops the operator submitting a form that would be refused.
  const phraseMatches = $derived(confirmText.trim() === phrase && phrase !== '');

  function open(verb: SecretVerb, service: DbCredentialStatus) {
    pendingVerb = verb;
    pendingService = service;
    confirmText = '';
    dbUser = '';
    dbPass = '';
  }

  function close() {
    pendingVerb = null;
    pendingService = null;
  }

  const dialogSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = actionPayload<{ action?: { ok: boolean; notice: string }; error?: string }>(result);
      if (result.type === 'success' && p?.action?.ok) {
        toast('ok', p.action.notice);
        close();
        // Reconcile with Doppler's view rather than guessing locally: a rotate
        // mints a user name only the server saw.
        bundle = null;
        await invalidateAll();
        return;
      }
      failed(p, t('admin.secrets.actionFailed'));
    };
  };

  // ── Local secret generator (never leaves the browser) ─────────────────────
  const GEN_KINDS = ['base64', 'hex', 'password'] as const;
  type GenKind = (typeof GEN_KINDS)[number];

  const GEN_LABEL = {
    base64: 'admin.secrets.genBase64',
    hex: 'admin.secrets.genHex',
    password: 'admin.secrets.genPassword'
  } as const satisfies Record<GenKind, string>;

  // No characters that need escaping in a MySQL connection string or a shell
  // one-liner: a generated password is going to be pasted into both.
  const PASSWORD_ALPHABET =
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~!*';
  const BYTES = 32;
  const PASSWORD_LENGTH = 40;

  let genKind = $state<GenKind>('base64');
  let generated = $state('');
  let genCopied = $state(false);

  function randomBytes(n: number): Uint8Array {
    return crypto.getRandomValues(new Uint8Array(n));
  }

  function generateValue(kind: GenKind): string {
    if (kind === 'password') {
      return [...randomBytes(PASSWORD_LENGTH)]
        .map((b) => PASSWORD_ALPHABET[b % PASSWORD_ALPHABET.length])
        .join('');
    }
    const bytes = randomBytes(BYTES);
    if (kind === 'hex') return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
    return btoa(String.fromCharCode(...bytes));
  }

  function generate() {
    generated = generateValue(genKind);
    genCopied = false;
  }

  const copyGenerated = () => copyFlash(generated, (on: boolean) => (genCopied = on));
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.secrets.eyebrow')} description={t('admin.secrets.description')}>
    {t('admin.secrets.titlePre')}<em>{t('admin.secrets.titleEm')}</em>
  </PageHead>

  {#if bundle === null}
    <SkeletonStack rows={4} height="220px" columns={2} />
  {:else}
    <div class="grid">
      {#each services as service (service.id)}
        <ServiceCard
          {service}
          {canManage}
          onRotate={() => open('rotate', service)}
          onSet={() => open('set', service)}
          onRevoke={() => open('revoke', service)}
        />
      {/each}

      <!-- Local generator: strong random material without any server round trip. -->
      <Card class="gen-card">
        <CardHead title={t('admin.secrets.genTitle')}>
          {#snippet action()}
            <StatePill tone="free">{t('admin.secrets.genLocal')}</StatePill>
          {/snippet}
        </CardHead>
        <p class="note">{t('admin.secrets.genNote')}</p>
        <div class="kinds">
          {#each GEN_KINDS as kind (kind)}
            <Chip on={genKind === kind} onclick={() => (genKind = kind)}>
              {t(GEN_LABEL[kind])}
            </Chip>
          {/each}
        </div>
        <div class="gen-row">
          <Button variant="primary" onclick={generate}>{t('admin.secrets.generate')}</Button>
          {#if generated}
            <input class="text-input" type="text" readonly value={generated} />
            <Button variant="ghost" onclick={copyGenerated}>
              {genCopied ? t('common.copied') : t('common.copy')}
            </Button>
          {/if}
        </div>
      </Card>
    </div>
  {/if}
</section>

<!-- One dialog for every secret mutation; the phrase check mirrors the server's. -->
<ConfirmDialog
  open={dialog !== null}
  title={dialog ? t(dialog.title) : ''}
  confirmLabel={dialog ? t(dialog.cta) : t('common.done')}
  cancelLabel={t('common.cancel')}
  danger={dialog?.danger ?? false}
  {busy}
  onCancel={close}
  onConfirm={() => dialogForm?.requestSubmit()}
>
  {#if dialog && pendingService}
    <div class="fields">
      <p class="note">
        {t(dialog.body, {
          schema: pendingService.schema,
          project: pendingService.project,
          config: pendingService.config,
          service: pendingService.label
        })}
      </p>

      {#if dialog.needsUser}
        <Field label={t('admin.secrets.fieldDbUser')}>
          <input
            class="text-input"
            type="text"
            autocomplete="off"
            placeholder={`${pendingService.expectedUserPrefix}_…`}
            bind:value={dbUser}
          />
        </Field>
      {/if}

      {#if dialog.needsPassword}
        <!-- A password field, not a text one: this dialog is opened on a shared
             operator screen often enough that the value should not be shoulder-
             readable, and the generator above is where it comes from anyway. -->
        <Field label={t('admin.secrets.fieldDbPass')}>
          <input class="text-input" type="password" autocomplete="new-password" bind:value={dbPass} />
        </Field>
      {/if}

      <Field label={t('admin.secrets.fieldConfirm', { phrase })}>
        <input class="text-input" type="text" autocomplete="off" bind:value={confirmText} />
      </Field>
      {#if !phraseMatches}
        <p class="note quiet">{t('admin.secrets.confirmHint')}</p>
      {/if}
    </div>
  {/if}
</ConfirmDialog>

{#if dialog && pendingService}
  <form
    method="POST"
    action={dialog.action}
    use:enhance={dialogSubmit}
    bind:this={dialogForm}
    hidden
  >
    <input type="hidden" name="service" value={pendingService.id} />
    <input type="hidden" name="confirm" value={confirmText} />
    <input type="hidden" name="db_user" value={dbUser} />
    <input type="hidden" name="db_pass" value={dbPass} />
  </form>
{/if}

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 16px;
  }

  .note {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.55;
    color: var(--bb-muted);
    margin: 0 0 12px;
  }
  .note.quiet {
    opacity: 0.75;
    margin: 0;
  }

  .kinds {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .gen-row {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }
  .gen-row .text-input {
    flex: 1;
    min-width: 140px;
  }

  :global(.gen-card) {
    border-style: dashed;
  }

  .fields {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin: 12px 0 4px;
  }
</style>
