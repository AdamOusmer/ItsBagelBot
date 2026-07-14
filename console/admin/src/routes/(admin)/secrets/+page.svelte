<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Button,
    PageHead,
    AlertBanner,
    ConfirmDialog,
    Modal,
    Skeleton,
    toast
  } from '@bagel/shared';
  import type { DbCredentialStatus, ServiceTokenView } from '$lib/server/secrets';
  import type { SecretsBundle } from './+page.server';

  let { data } = $props();

  // Streamed bundle -> local state; refreshed via invalidateAll after writes.
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
  const scope = $derived(bundle?.scope ?? null);

  // ── Dialog state machine ───────────────────────────────────────────────────
  type PendingKind = 'rotate' | 'set' | 'revoke' | 'mint' | 'revokeToken';
  type Pending = { kind: PendingKind; svc: DbCredentialStatus; token?: ServiceTokenView };
  let pending = $state<Pending | null>(null);

  let confirmText = $state('');
  let dbUser = $state('');
  let dbPass = $state('');
  let tokenName = $state('');
  let tokenExpiry = $state('90');
  let busy = $state(false);

  function open(kind: PendingKind, svc: DbCredentialStatus, token?: ServiceTokenView) {
    pending = { kind, svc, token };
    confirmText = '';
    dbUser = '';
    dbPass = '';
    tokenName = `${svc.id}-readonly`;
    tokenExpiry = '90';
  }

  function close() {
    pending = null;
  }

  const phrase = $derived.by(() => {
    if (!pending) return '';
    switch (pending.kind) {
      case 'rotate':
        return `rotate ${pending.svc.id}`;
      case 'set':
        return `set ${pending.svc.id}`;
      case 'revoke':
        return `revoke ${dbUser.trim()}`;
      case 'mint':
        return `mint ${pending.svc.id}`;
      case 'revokeToken':
        return `revoke ${pending.svc.id} token`;
    }
  });

  const DIALOG_META: Record<PendingKind, { title: string; action: string; danger: boolean; cta: string }> = {
    rotate: { title: 'Rotate database credential', action: '?/rotate', danger: false, cta: 'Rotate' },
    set: { title: 'Set database credential', action: '?/set', danger: false, cta: 'Set credential' },
    revoke: { title: 'Revoke database user', action: '?/revoke', danger: true, cta: 'Revoke' },
    mint: { title: 'Mint read-only Doppler token', action: '?/mintToken', danger: false, cta: 'Mint token' },
    revokeToken: { title: 'Revoke Doppler service token', action: '?/revokeToken', danger: true, cta: 'Revoke token' }
  };

  let dialogForm = $state<HTMLFormElement | null>(null);

  // Minted keys are shown exactly once — the server never stores them.
  let mintedKey = $state('');
  let mintedCopied = $state(false);

  type ActionPayload = {
    action?: { ok: boolean; notice: string };
    mintedKey?: string;
    error?: string;
  };

  const dialogSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const r = result as { type: string; data?: ActionPayload };
      const p = r.type === 'success' || r.type === 'failure' ? r.data : undefined;
      if (r.type === 'success' && p?.action?.ok) {
        toast('ok', p.action.notice);
        close();
        if (p.mintedKey) {
          mintedKey = p.mintedKey;
          mintedCopied = false;
        }
        // Reconcile with Doppler's view rather than guessing locally.
        bundle = null;
        await invalidateAll();
        return;
      }
      toast('err', p?.error ?? p?.action?.notice ?? 'action failed');
    };
  };

  async function copyMinted() {
    try {
      await navigator.clipboard.writeText(mintedKey);
      mintedCopied = true;
      setTimeout(() => (mintedCopied = false), 1500);
    } catch {
      mintedCopied = false;
    }
  }

  // ── Local secret generator (never leaves the browser) ─────────────────────
  type GenKind = 'base64' | 'hex' | 'password';
  let genKind = $state<GenKind>('base64');
  let generated = $state('');
  let genCopied = $state(false);

  const PASSWORD_ALPHABET =
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~!*';

  function generate() {
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    if (genKind === 'base64') {
      generated = btoa(String.fromCharCode(...bytes));
    } else if (genKind === 'hex') {
      generated = [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
    } else {
      const idx = crypto.getRandomValues(new Uint8Array(40));
      generated = [...idx].map((b) => PASSWORD_ALPHABET[b % PASSWORD_ALPHABET.length]).join('');
    }
    genCopied = false;
  }

  async function copyGenerated() {
    try {
      await navigator.clipboard.writeText(generated);
      genCopied = true;
      setTimeout(() => (genCopied = false), 1500);
    } catch {
      genCopied = false;
    }
  }

  function fmtDate(iso: string | null): string {
    if (!iso) return 'never';
    return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  }

  const SOURCE_META: Record<string, { label: string; cls: string }> = {
    scoped: { label: 'scoped token', cls: 'ok' },
    legacy: { label: 'legacy broad token', cls: 'warn' },
    missing: { label: 'no token', cls: 'err' }
  };
</script>

<section class="screen active">
  <PageHead
    eyebrow="Access control"
    description="Runtime database users and Doppler service tokens, minted least-privileged."
  >
    Service <em>secrets</em>
  </PageHead>

  {#if bundle === null}
    <div class="loading-stack">
      <Skeleton variant="block" height="60px" />
      <Skeleton variant="block" height="200px" />
      <Skeleton variant="block" height="200px" />
    </div>
  {:else}
    {#if scope?.legacyInUse}
      <AlertBanner>
        {Object.values(scope.sources).filter((s) => s === 'legacy').length} of {services.length}
        services still use the broad legacy Doppler token
        {#if scope.legacyExcessProjects.length}
          — it can also reach {scope.legacyExcessProjects.join(', ')}
        {/if}. Set DOPPLER_TOKEN_&lt;SERVICE&gt; per project to finish the least-privilege migration.
      </AlertBanner>
    {:else if scope}
      <p class="scope-ok">
        <Icon name="check" size={13} /> Every service resolves a per-project scoped Doppler token.
      </p>
    {/if}

    <div class="svc-grid">
      {#each services as svc (svc.id)}
        {@const src = SOURCE_META[svc.tokenSource] ?? SOURCE_META.missing}
        {@const tokens = bundle.tokens[svc.id] ?? []}
        <div class="card svc-card">
          <div class="card-head">
            <h3>{svc.label}</h3>
            <span class="src-badge {src.cls}">{src.label}</span>
          </div>

          <dl class="svc-facts">
            <div><dt>Doppler</dt><dd>{svc.project}/{svc.config}</dd></div>
            <div><dt>Schema</dt><dd>{svc.schema}</dd></div>
            <div>
              <dt>DB user</dt>
              <dd class:missing={!svc.dbUser}>
                {#if svc.canReadDoppler}{svc.dbUser || 'not set'}{:else}unreadable (token?){/if}
              </dd>
            </div>
            <div><dt>Auto-migrate</dt><dd>{svc.autoMigrate || '—'}</dd></div>
          </dl>

          <div class="svc-actions">
            <Button variant="ghost" onclick={() => open('rotate', svc)}>Rotate</Button>
            <Button variant="ghost" onclick={() => open('set', svc)}>Set…</Button>
            <Button variant="ghost" class="danger" onclick={() => open('revoke', svc)}>Revoke user…</Button>
          </div>

          <div class="tok-block">
            <div class="tok-head">
              <span class="tok-label">Service tokens (read-only, this config only)</span>
              <button class="mini-act" type="button" title="Mint token" aria-label="Mint token for {svc.label}" onclick={() => open('mint', svc)}>
                <Icon name="plus" size={13} />
              </button>
            </div>
            {#if tokens.length === 0}
              <p class="tok-empty">None issued.</p>
            {:else}
              <ul class="tok-list">
                {#each tokens as t (t.slug)}
                  <li class="tok-row">
                    <span class="tok-name">{t.name}</span>
                    <span class="tok-meta">
                      created {fmtDate(t.createdAt)} · last seen {fmtDate(t.lastSeenAt)}
                      {#if t.expiresAt}· expires {fmtDate(t.expiresAt)}{/if}
                    </span>
                    <button
                      class="mini-act danger"
                      type="button"
                      title="Revoke token"
                      aria-label="Revoke token {t.name}"
                      onclick={() => open('revokeToken', svc, t)}
                    >
                      <Icon name="trash" size={13} />
                    </button>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        </div>
      {/each}

      <!-- Local generator: strong random material without any server round trip. -->
      <div class="card svc-card gen-card">
        <div class="card-head">
          <h3>Secret generator</h3>
          <span class="src-badge ok">local only</span>
        </div>
        <p class="gen-note">
          Generated with your browser's CSPRNG; nothing here is sent to any server.
        </p>
        <div class="gen-kinds">
          {#each ['base64', 'hex', 'password'] as k (k)}
            <button type="button" class="chip" class:on={genKind === k} onclick={() => (genKind = k as GenKind)}>
              {k === 'base64' ? '32B base64' : k === 'hex' ? '32B hex' : '40-char password'}
            </button>
          {/each}
        </div>
        <div class="gen-row">
          <Button variant="primary" onclick={generate}>Generate</Button>
          {#if generated}
            <input class="text-input mono" type="text" readonly value={generated} />
            <Button variant="ghost" onclick={copyGenerated}>{genCopied ? 'Copied' : 'Copy'}</Button>
          {/if}
        </div>
      </div>
    </div>
  {/if}
</section>

<!-- One dialog for every secret mutation; the phrase check mirrors the server's. -->
<ConfirmDialog
  open={pending !== null}
  title={pending ? DIALOG_META[pending.kind].title : ''}
  confirmLabel={pending ? DIALOG_META[pending.kind].cta : 'Confirm'}
  cancelLabel="Cancel"
  danger={pending ? DIALOG_META[pending.kind].danger : false}
  busy={busy}
  onCancel={close}
  onConfirm={() => dialogForm?.requestSubmit()}
>
  {#if pending}
    <div class="dialog-fields">
      {#if pending.kind === 'rotate'}
        <p class="dialog-note">
          Provisions a fresh MySQL user for <b>{pending.svc.schema}</b>, writes it to Doppler
          ({pending.svc.project}/{pending.svc.config}), and lets the operator reload pick it up.
          The old user stays until you revoke it.
        </p>
      {:else if pending.kind === 'set'}
        <p class="dialog-note">
          Provisions the named MySQL user with data-only grants on <b>{pending.svc.schema}</b> and
          writes it to Doppler with auto-migrate on.
        </p>
        <label>Database user
          <input class="text-input mono" type="text" bind:value={dbUser} placeholder="{pending.svc.expectedUserPrefix}_…" />
        </label>
        <label>Password (32-128 chars)
          <input class="text-input mono" type="password" bind:value={dbPass} autocomplete="new-password" />
        </label>
      {:else if pending.kind === 'revoke'}
        <p class="dialog-note">
          Drops the MySQL user and every grant it holds. Revoke only retired users — the service
          crashes if you drop the one in Doppler.
        </p>
        <label>Database user to revoke
          <input class="text-input mono" type="text" bind:value={dbUser} placeholder="{pending.svc.expectedUserPrefix}_…" />
        </label>
      {:else if pending.kind === 'mint'}
        <p class="dialog-note">
          Issues a Doppler service token that can only <b>read {pending.svc.project}/{pending.svc.config}</b> —
          the narrowest credential Doppler can mint. The key is shown once.
        </p>
        <label>Token name
          <input class="text-input mono" type="text" bind:value={tokenName} />
        </label>
        <label>Expires in (days, 0 = never)
          <input class="text-input mono" type="number" min="0" max="365" bind:value={tokenExpiry} />
        </label>
      {:else if pending.kind === 'revokeToken'}
        <p class="dialog-note">
          Revokes <b>{pending.token?.name}</b>. Anything still using it loses read access to
          {pending.svc.project}/{pending.svc.config} immediately.
        </p>
      {/if}

      <label>Type <b class="mono">{phrase}</b> to confirm
        <input class="text-input mono" type="text" bind:value={confirmText} autocomplete="off" />
      </label>
    </div>
  {/if}
</ConfirmDialog>

{#if pending}
  <form
    method="POST"
    action={DIALOG_META[pending.kind].action}
    use:enhance={dialogSubmit}
    bind:this={dialogForm}
    hidden
  >
    <input type="hidden" name="service" value={pending.svc.id} />
    <input type="hidden" name="confirm" value={confirmText} />
    <input type="hidden" name="db_user" value={dbUser} />
    <input type="hidden" name="db_pass" value={dbPass} />
    <input type="hidden" name="name" value={tokenName} />
    <input type="hidden" name="expire_days" value={tokenExpiry} />
    <input type="hidden" name="slug" value={pending.token?.slug ?? ''} />
  </form>
{/if}

<!-- Minted key reveal: shown exactly once, never stored server-side. -->
<Modal open={mintedKey !== ''} title="Copy the token now" closeModal={() => (mintedKey = '')}>
  <p class="dialog-note">
    This is the only time the key is shown. Doppler does not let anyone read it again.
  </p>
  <div class="gen-row">
    <input class="text-input mono" type="text" readonly value={mintedKey} />
    <Button variant="primary" onclick={copyMinted}>{mintedCopied ? 'Copied' : 'Copy'}</Button>
  </div>
  <div class="modal-actions">
    <button type="button" class="btn ghost" onclick={() => (mintedKey = '')}>Done</button>
  </div>
</Modal>

<style>
  .loading-stack { display: flex; flex-direction: column; gap: 14px; }

  .scope-ok {
    display: inline-flex; align-items: center; gap: 8px;
    font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-green-glow);
    margin: 0 0 16px;
  }
  .scope-ok :global(svg) { stroke: currentColor; fill: none; }

  .svc-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 16px;
  }
  .svc-card { display: flex; flex-direction: column; gap: 14px; }

  .src-badge {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase;
    padding: 2px 9px; border-radius: var(--bb-radius-pill); border: 1px solid transparent; white-space: nowrap;
  }
  .src-badge.ok { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.1); border-color: rgba(82, 183, 136, 0.3); }
  .src-badge.warn { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.1); border-color: rgba(201, 168, 124, 0.3); }
  .src-badge.err { color: #cf8a78; background: rgba(176, 90, 70, 0.1); border-color: rgba(176, 90, 70, 0.3); }

  .svc-facts { display: flex; flex-direction: column; gap: 8px; margin: 0; }
  .svc-facts div { display: flex; justify-content: space-between; gap: 12px; align-items: baseline; }
  .svc-facts dt { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  .svc-facts dd {
    margin: 0; font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-tan-light);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .svc-facts dd.missing { color: #cf8a78; }

  .svc-actions { display: flex; gap: 8px; flex-wrap: wrap; }
  .svc-actions :global(.danger) { color: #cf8a78; border-color: rgba(176, 90, 70, 0.4); }

  .tok-block { border-top: 1px solid var(--rule); padding-top: 12px; }
  .tok-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 8px; }
  .tok-label {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.1em;
    text-transform: uppercase; color: var(--bb-muted);
  }
  .tok-empty { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); margin: 0; }
  .tok-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .tok-row { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 10px; align-items: center; }
  .tok-name { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-white); }
  .tok-meta {
    font-family: var(--bb-font-mono); font-size: 10.5px; color: var(--bb-muted);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  .mini-act {
    width: 26px; height: 26px; border-radius: 7px;
    display: inline-flex; align-items: center; justify-content: center;
    background: none; border: 1px solid transparent; color: var(--bb-muted); cursor: pointer;
  }
  .mini-act :global(svg) { stroke: currentColor; fill: none; stroke-width: 1.7; }
  .mini-act:hover { color: var(--bb-white); background: rgba(255, 255, 255, 0.05); }
  .mini-act.danger:hover { color: #cf8a78; background: rgba(176, 90, 70, 0.1); }

  .gen-card { border-style: dashed; }
  .gen-note { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); margin: 0; }
  .gen-kinds { display: flex; gap: 6px; flex-wrap: wrap; }
  .chip {
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em;
    padding: 7px 14px; border-radius: var(--bb-radius-pill); white-space: nowrap;
    background: rgba(255, 255, 255, 0.03); border: 1px solid var(--glass-border);
    color: var(--bb-muted); cursor: pointer;
  }
  .chip:hover { color: var(--bb-white); border-color: var(--bb-border-strong); }
  .chip.on { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--bb-border-strong); }
  .gen-row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }

  .text-input {
    flex: 1; min-width: 0; padding: 8px 11px;
    font-family: var(--bb-font-body); font-size: 13px;
    border: 1px solid var(--rule); border-radius: 8px;
    background: var(--bb-bg-1, #16130f); color: var(--bb-white);
  }
  .text-input.mono { font-family: var(--bb-font-mono); font-size: 12px; }
  .text-input:focus { outline: none; border-color: var(--bb-border-strong); }

  .dialog-fields { display: flex; flex-direction: column; gap: 12px; margin: 12px 0 4px; }
  .dialog-fields label {
    display: flex; flex-direction: column; gap: 6px;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted);
  }
  .dialog-note { font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55; color: var(--bb-muted); margin: 0; }
  .dialog-note b { color: var(--bb-white); font-weight: 600; }
  .mono { font-family: var(--bb-font-mono); }
</style>
