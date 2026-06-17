<script lang="ts">
  import { Icon } from '@bagel/shared';
  import { page } from '$app/state';
  import type { DelegationGrant } from '$lib/server/rpc';

  let { data, form } = $props();

  const grants = $derived((data.grants ?? []) as DelegationGrant[]);
  const origin = $derived(page.url.origin);

  function linkFor(token: string): string {
    return `${origin}/delegate/accept?t=${token}`;
  }

  async function copy(token: string) {
    try {
      await navigator.clipboard.writeText(linkFor(token));
    } catch {
      /* clipboard blocked; user can select manually */
    }
  }
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Share</span>
    <h1>Delegated <em>access</em></h1>
    <p>Generate a single-use link that grants someone scoped access to parts of your dashboard. Each link works exactly once.</p>
  </div>

  {#if form?.error}
    <p class="banner err">{form.error}</p>
  {:else if form?.ok && form.action === 'created'}
    <p class="banner ok">Link created. Copy it from the list below.</p>
  {:else if form?.ok && form.action === 'revoked'}
    <p class="banner ok">Link revoked.</p>
  {/if}

  <form method="POST" action="?/create" class="card create">
    <h2>New share link</h2>
    <p class="hint">Pick which sections the invitee can manage.</p>
    <label class="chk"><input type="checkbox" name="commands" /> Commands</label>
    <label class="chk"><input type="checkbox" name="modules" /> Modules</label>
    <button class="btn primary" type="submit"><Icon name="commands" size={14} /> Generate link</button>
  </form>

  <div class="card list">
    <h2>Existing links</h2>
    {#if grants.length === 0}
      <p class="hint">No links yet.</p>
    {:else}
      <table>
        <thead>
          <tr><th>Sections</th><th>Status</th><th>Link</th><th></th></tr>
        </thead>
        <tbody>
          {#each grants as g (g.token)}
            <tr>
              <td>{g.sections.join(', ')}</td>
              <td>
                {#if g.consumed}
                  <span class="tag used">Used by {g.delegate_login || 'unknown'}</span>
                {:else}
                  <span class="tag open">Unused</span>
                {/if}
              </td>
              <td class="linkcell">
                {#if g.consumed}
                  <span class="muted">—</span>
                {:else}
                  <code>{linkFor(g.token)}</code>
                  <button type="button" class="btn ghost sm" onclick={() => copy(g.token)}>Copy</button>
                {/if}
              </td>
              <td>
                <form method="POST" action="?/revoke">
                  <input type="hidden" name="token" value={g.token} />
                  <button type="submit" class="btn ghost sm danger">Revoke</button>
                </form>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</section>

<style>
  .card {
    background: var(--bb-bg-2, #1a1714);
    border: 1px solid var(--bb-line, rgba(255, 255, 255, 0.08));
    border-radius: var(--bb-radius, 14px);
    padding: 20px;
    margin-top: 18px;
  }
  .card h2 { margin: 0 0 6px; font-size: 16px; }
  .hint { color: var(--bb-muted, #998f82); font-size: 13px; margin: 0 0 12px; }
  .chk { display: block; margin: 6px 0; font-size: 14px; cursor: pointer; }
  .chk input { margin-right: 8px; }
  .create .btn { margin-top: 14px; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06)); }
  th { color: var(--bb-muted, #998f82); font-weight: 600; }
  .linkcell code { font-size: 12px; word-break: break-all; }
  .muted { color: var(--bb-muted, #998f82); }
  .tag { padding: 2px 8px; border-radius: 999px; font-size: 12px; }
  .tag.open { background: rgba(120, 200, 120, 0.18); color: #8fd08f; }
  .tag.used { background: rgba(200, 160, 120, 0.18); color: #c9a87c; }
  .btn.sm { padding: 4px 10px; font-size: 12px; margin-left: 8px; }
  .btn.danger { color: #e08f8f; }
  .banner { padding: 10px 14px; border-radius: 10px; font-size: 13px; margin-top: 14px; }
  .banner.err { background: rgba(220, 120, 120, 0.16); color: #e08f8f; }
  .banner.ok { background: rgba(120, 200, 120, 0.16); color: #8fd08f; }
</style>
