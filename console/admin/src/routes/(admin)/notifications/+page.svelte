<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, PageHead, Card, CardHead, Button, EmptyState, ConfirmDialog, RadioGroup, toast } from '@bagel/shared';
  import type { NotificationWire } from '$lib/server/services';
  let { data, form } = $props();

  const notifications = $derived((data.notifications ?? []) as NotificationWire[]);
  const page = $derived(Number(data.page ?? 1));
  const maxPages = $derived(Number(data.maxPages ?? 25));
  const hasMore = $derived(Boolean(data.hasMore));

  let scope = $state<'broadcast' | 'direct'>('broadcast');
  let level = $state('info');

  function notificationsHref(pageNo: number): string {
    return pageNo > 1 ? `/notifications?page=${pageNo}` : '/notifications';
  }

  function levelLabel(l: string): string {
    return l.charAt(0).toUpperCase() + l.slice(1);
  }

  // Surface action results as toasts and reset the compose form on success.
  // svelte-ignore state_referenced_locally
  let lastForm: unknown = form;
  let composeForm = $state<HTMLFormElement | null>(null);
  $effect(() => {
    if (form === lastForm) return;
    lastForm = form;
    const action = (form as { action?: { ok: boolean; notice: string } } | undefined)?.action;
    if (!action) return;
    if (action.ok) {
      toast('ok', action.notice);
      composeForm?.reset();
    } else {
      toast('err', action.notice);
    }
  });

  let retractTarget = $state<NotificationWire | null>(null);
  let retractForm = $state<HTMLFormElement | null>(null);
</script>

<section class="screen active">
  <PageHead eyebrow="Operate" description="Compose a message for one user or every user; it appears in their dashboard.">
    <em>Notifications</em>
  </PageHead>

  <Card class="notif-card">
    <CardHead title="Compose" />
    <form method="POST" action="?/send" class="compose" use:enhance bind:this={composeForm}>
      <RadioGroup
        name="scope"
        label="Audience"
        bind:value={scope}
        options={[
          { value: 'broadcast', label: 'Broadcast to everyone' },
          { value: 'direct', label: 'Direct to one user' }
        ]}
      />

      {#if scope === 'direct'}
        <div class="row two">
          <label>
            User ID
            <input type="text" name="target_user_id" placeholder="e.g. 44322190" autocomplete="off" />
          </label>
          <label>
            or username
            <input type="text" name="target_username" placeholder="e.g. itsmavey" autocomplete="off" />
          </label>
        </div>
      {/if}

      <label class="field">
        Title
        <input type="text" name="title" maxlength="120" required placeholder="Scheduled maintenance tonight" />
      </label>

      <label class="field">
        Message
        <textarea name="body" maxlength="2000" required rows="3" placeholder="What do you want them to know?"></textarea>
      </label>

      <div class="row two">
        <label>
          Level
          <select name="level" bind:value={level}>
            <option value="info">Info</option>
            <option value="success">Success</option>
            <option value="warning">Warning</option>
            <option value="critical">Critical</option>
          </select>
        </label>
        <label>
          Expires (optional)
          <input type="datetime-local" name="expires_at" />
        </label>
      </div>

      <Button type="submit" variant="primary" icon="send">Send notification</Button>
    </form>
  </Card>

  <Card class="notif-card">
    <CardHead title="Sent" />
    {#if notifications.length === 0}
      <EmptyState icon="bell" title="No notifications yet" body="Notifications you send appear here." />
    {:else}
      <div class="list">
        {#each notifications as n (n.id)}
          <div class="row-item">
            <div class="row-main">
              <span class="level {n.level}">{levelLabel(n.level)}</span>
              <div class="text">
                <b>{n.title}</b>
                <p>{n.body}</p>
                <span class="meta">
                  {n.scope === 'broadcast' ? 'All users' : `User ${n.target_user_id}`} · sent by {n.created_by_login} ·
                  {new Date(n.created_at).toLocaleString()}
                  {#if n.expires_at}· expires {new Date(n.expires_at).toLocaleString()}{/if}
                </span>
              </div>
            </div>
            <button type="button" class="btn ghost sm danger" onclick={() => (retractTarget = n)}>
              <Icon name="trash" size={12} /> Retract
            </button>
          </div>
        {/each}
      </div>

      {#if page > 1 || hasMore}
        <div class="pager">
          <a class="btn ghost sm" class:disabled={page <= 1} href={notificationsHref(page - 1)}>Prev</a>
          <span class="page-no">Page {page} / {maxPages}</span>
          <a class="btn ghost sm" class:disabled={!hasMore} href={notificationsHref(page + 1)}>Next</a>
        </div>
      {/if}
    {/if}
  </Card>
</section>

<ConfirmDialog
  open={retractTarget !== null}
  title="Retract this notification?"
  body="It disappears from every recipient's dashboard immediately. This cannot be undone."
  confirmLabel="Retract"
  danger
  onCancel={() => (retractTarget = null)}
  onConfirm={() => {
    retractForm?.requestSubmit();
    retractTarget = null;
  }}
/>
{#if retractTarget}
  <form method="POST" action="?/delete" use:enhance bind:this={retractForm} hidden>
    <input type="hidden" name="id" value={retractTarget.id} />
  </form>
{/if}

<style>
  :global(.notif-card) { margin-top: 18px; }

  .compose { display: flex; flex-direction: column; gap: 14px; }
  .row { display: flex; gap: 14px; flex-wrap: wrap; }
  .row.two > label { flex: 1; min-width: 180px; }

  label.field, .row label {
    display: flex; flex-direction: column; gap: 6px;
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em; text-transform: uppercase; color: var(--bb-muted);
  }
  input[type='text'], input[type='datetime-local'], textarea, select {
    font-family: var(--bb-font-body); font-size: 14px; text-transform: none; letter-spacing: normal;
    color: var(--bb-white); background: rgba(255,255,255,0.03); border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-sm, 8px); padding: 9px 12px;
  }
  textarea { resize: vertical; font-family: var(--bb-font-body); }

  .list { display: flex; flex-direction: column; gap: 10px; }
  .row-item {
    display: flex; align-items: flex-start; justify-content: space-between; gap: 14px;
    border: 1px solid var(--glass-border); border-radius: var(--bb-radius-md, 10px);
    padding: 12px 14px; background: rgba(255, 255, 255, 0.02);
  }
  .row-main { display: flex; gap: 12px; align-items: flex-start; flex: 1; min-width: 0; }
  .text b { font-size: 14px; color: var(--bb-white); }
  .text p { margin: 4px 0; font-size: 13px; color: var(--bb-muted); }
  .meta { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); opacity: 0.8; }

  .level {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase;
    padding: 4px 10px; border-radius: var(--bb-radius-pill); border: 1px solid transparent; white-space: nowrap;
  }
  .level.info { background: rgba(255,255,255,0.04); color: var(--bb-muted); border-color: var(--glass-border); }
  .level.success { background: rgba(82,183,136,0.10); color: var(--bb-green-glow); border-color: rgba(82,183,136,0.28); }
  .level.warning { background: rgba(201,168,124,0.10); color: var(--bb-tan-light); border-color: rgba(201,168,124,0.28); }
  .level.critical { background: rgba(176,90,70,0.15); color: #cf8a78; border-color: rgba(176,90,70,0.4); }

  .btn.sm { padding: 4px 10px; font-size: 12px; }
  .btn.danger { color: #e08f8f; }

  .pager { display: flex; align-items: center; gap: 12px; margin-top: 14px; }
  .page-no { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); }
  .disabled { pointer-events: none; opacity: 0.4; }

  @media (max-width: 760px) {
    .row-item { flex-direction: column; }
  }
</style>
