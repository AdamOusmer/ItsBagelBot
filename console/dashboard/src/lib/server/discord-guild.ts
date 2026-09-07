// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The guild shell: one load and one action table, shared by /discord/[guildId]
// and every page under it.
//
// It lives in $lib/server rather than in the route because the section is now
// seven routes, not one. The load runs in +layout.server.ts so switching from
// Channels to Tickets does not re-fetch the guild's layout and status (a
// +page.server.ts load would re-run on every sub-navigation), and the action
// table is re-exported by each child +page.server.ts so a form posted from any
// sub-page reaches the same save/setup/repost/disconnect implementations. Both
// halves being here is what stops the seven routes drifting into seven
// slightly different ownership checks.
//
// Everything is scoped to the id in the URL, and that id is never trusted: it
// has to appear in this broadcaster's own guilds.list or the route 404s, on
// the load AND on every action.
import type { Actions, RequestEvent, ServerLoadEvent } from '@sveltejs/kit';
import {
  blankDiscordConfig,
  blankLayout,
  blankStatus,
  botStatus,
  guildLayout,
  listGuilds,
  persistSetup,
  pinnedRolesOf,
  readDiscord,
  readGuildConfig,
  readLegacyBlob,
  repostDesk,
  saveDiscordModule,
  saveGuildConfig,
  setupGuild,
  unbindGuild,
  type DiscordConfig,
  type DiscordGuildSummary,
  type DiscordGuildTarget,
  type DiscordLayout,
  type DiscordStatus
} from '$lib/server/discord-store';
import { discordConfigured, requireDiscordActor } from '$lib/server/discord-oauth';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { assertModuleUnlocked, gateModulePage, moduleLocked } from '$lib/server/module-gate';
import { DISCORD_DEF } from '$lib/server/discord-def';
import {
  DISCORD_CONFIG_VERSION_NEW,
  alertOff,
  alertOn,
  clearPinnedSlots,
  droppedPinNotice,
  legacyConfigFor,
  mergeDiscordConfig,
  type PinnedSlot
} from '@bagel/shared';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { error, fail, isRedirect, redirect, type Cookies } from '@sveltejs/kit';

// process.env, not $env/dynamic/private: this route sits behind guard.ts on
// the boot import graph (see module-gate.ts).
const DEMO = dev && process.env.DEMO === '1';

function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'discord');
}

export type DiscordGuildPage = {
  locked: boolean;
  enabled: boolean;
  guildId: string;
  guilds: DiscordGuildSummary[];
  config: DiscordConfig;
  version: number;
  found: boolean;
  layout: DiscordLayout;
  status: DiscordStatus;
  configured: boolean;
  justConnected: boolean;
  refused: boolean;
  droppedPins: PinnedSlot[];
  degraded: boolean;
};

function blankPage(guildId: string, locked: boolean): DiscordGuildPage {
  return {
    locked,
    enabled: false,
    guildId,
    guilds: [],
    config: { ...blankDiscordConfig(), guildId },
    version: 0,
    found: false,
    layout: blankLayout(),
    status: blankStatus(),
    configured: discordConfigured(),
    justConnected: false,
    refused: false,
    droppedPins: [],
    degraded: false
  };
}

/**
 * The ownership check, and the only one.
 *
 * guildId is a path segment, so it is caller-supplied: without this any
 * signed-in broadcaster could read (and save) another one's server just by
 * typing its id. `guilds.list` is derived from the bindings outgress owns, so
 * membership in it IS the proof. 404 rather than 403 on purpose: a 403 would
 * confirm the guild exists and is somebody's.
 */
function ownedGuild(guilds: DiscordGuildSummary[], guildId: string): DiscordGuildSummary {
  const found = guilds.find((g) => g.guildId === guildId);
  if (!found) throw error(404, 'No such server.');
  return found;
}

/**
 * The dropped-pin notice, carried from the POST that found it to the load that
 * renders it.
 *
 * Setup finishes, the page calls invalidateAll(), and the action's payload is
 * gone before the banner has a frame to appear in -- so the notice has to
 * survive one round trip. A cookie rather than a query string because
 * `?dropped=mods,vip` rides along in every link the streamer copies and would
 * warn them again about a pin they fixed weeks ago; this one is scoped to the
 * guild's own page and deleted by the first load that reads it.
 */
const DROPPED_COOKIE = 'bb_discord_dropped';

function droppedPath(guildId: string): string {
  return `/discord/${guildId}`;
}

function rememberDroppedPins(cookies: Cookies, guildId: string, slots: PinnedSlot[]): void {
  if (slots.length === 0) return;
  cookies.set(DROPPED_COOKIE, slots.join(','), {
    path: droppedPath(guildId),
    httpOnly: true,
    sameSite: 'lax',
    // Long enough for the reload setup triggers, short enough that a tab left
    // open overnight does not greet the streamer with it tomorrow.
    maxAge: 300
  });
}

function takeDroppedPins(cookies: Cookies, guildId: string): PinnedSlot[] {
  const raw = cookies.get(DROPPED_COOKIE);
  if (!raw) return [];
  cookies.delete(DROPPED_COOKIE, { path: droppedPath(guildId) });
  return droppedPinNotice(raw.split(','));
}

export const loadGuildShell = async ({
  cookies,
  locals,
  params,
  url
}: ServerLoadEvent): Promise<DiscordGuildPage> => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  // The route shape guarantees the segment; the fallback is only here because
  // a generic ServerLoadEvent types params as partial.
  const guildId = params.guildId ?? '';
  const locked = await moduleLocked(locals, DISCORD_DEF);
  const droppedPins = takeDroppedPins(cookies, guildId);

  if (DEMO) return { ...(await demoPage(guildId, url)), droppedPins };

  const view = await readDiscord({ userId: uid }).catch(() => null);
  if (!view) return { ...blankPage(guildId, locked), degraded: true };
  ownedGuild(view.guilds, guildId);

  const target = { userId: uid, guildId };
  const flags = {
    justConnected: url.searchParams.get('connected') === '1',
    refused: url.searchParams.get('refused') === '1'
  };
  const reads = await guildReads(target);
  return {
    ...blankPage(guildId, locked),
    ...flags,
    droppedPins,
    enabled: view.enabled,
    guilds: view.guilds,
    ...reads,
    ...(await migrateLegacy(target, view.twitchLogin, view.enabled, reads))
  };
};

/**
 * Copies a pre-split board's config off the per-user modules blob.
 *
 * Before the multi-guild split (§H) the whole config lived in `MOD.discord`.
 * The blob is narrowed to `{twitchLogin}` on the first write after the split,
 * so a board that has not been touched since still holds every channel and
 * role id the streamer picked -- and the guild row holds none of them. First
 * load of the guild page is the moment to move them, because it is the first
 * moment we know WHICH guild the blob describes.
 *
 * Order is the whole point: the guild row is written first and the blob is
 * narrowed only after that write is acknowledged. A failure anywhere leaves
 * the blob intact and the page rendering defaults, so the next load tries
 * again rather than having quietly destroyed the only copy.
 */
async function migrateLegacy(
  target: DiscordGuildTarget,
  twitchLogin: string,
  enabled: boolean,
  reads: { config: DiscordConfig; version: number; found: boolean; degraded: boolean }
): Promise<{ config: DiscordConfig; version: number } | null> {
  if (reads.found || reads.degraded) return null;
  try {
    const legacy = legacyConfigFor(await readLegacyBlob({ userId: target.userId }), target.guildId);
    if (!legacy) return null;
    const config = { ...legacy, twitchLogin: twitchLogin || legacy.twitchLogin };
    const saved = await saveGuildConfig({ ...target, config, expectedVersion: DISCORD_CONFIG_VERSION_NEW });
    if (saved.error || saved.code) {
      logger.warn({ code: saved.code }, '[discord] legacy config migration refused');
      return null;
    }
    await saveDiscordModule({ userId: target.userId, enabled, twitchLogin });
    logger.info({ guildId: target.guildId }, '[discord] migrated the legacy module blob onto the guild row');
    return { config, version: saved.version };
  } catch (e) {
    logger.warn({ err: e }, '[discord] legacy config migration failed');
    return null;
  }
}

async function demoPage(guildId: string, url: URL): Promise<DiscordGuildPage> {
  const { demoDiscordView, demoDiscordConfig, demoDiscordLayout, demoDiscordStatus } = await import(
    '$lib/server/demo-data'
  );
  const view = demoDiscordView();
  const g = ownedGuild(view.guilds, guildId);
  const row = demoDiscordConfig();
  // The layout and status fixtures describe one server; re-stamping them with
  // the picked guild is what makes switching servers in demo show a different
  // server rather than the same card twice under two names.
  const guild = { id: guildId, name: g.name, iconUrl: '', memberCount: g.memberCount };
  return {
    ...blankPage(guildId, false),
    enabled: view.enabled,
    guilds: view.guilds,
    config: { ...row.config, guildId },
    version: row.version,
    found: row.found,
    // needsReauth is re-stamped from the picked guild for the same reason the
    // guild info is: the fixture describes one server, and without this the
    // demo guild whose grant is dead renders a healthy shell, which is the one
    // state the reauth banner exists for.
    layout: { ...demoDiscordLayout(), guild, botOnline: g.botPresent, needsReauth: g.needsReauth },
    status: {
      ...demoDiscordStatus(),
      guild,
      online: g.botPresent,
      guildPresent: g.botPresent,
      needsReauth: g.needsReauth
    },
    configured: true,
    justConnected: url.searchParams.get('connected') === '1',
    refused: url.searchParams.get('refused') === '1'
  };
}

/**
 * What an action answers with no outgress behind it.
 *
 * `save` runs the REAL merge, so the demo refuses exactly what production
 * refuses, plus one field standing in for a rule only the server can enforce:
 * editing the ticket panel title comes back `invalid`, which is the one way to
 * walk the fields[] path on a laptop. `setup` leaves the same one-shot cookie a
 * real fill would, so the dropped-pin banner is reachable the same way.
 */
async function demoOutcome(label: string, ctx: ActionCtx) {
  if (label === 'save') return demoSave(ctx);
  if (label === 'setup') {
    const { demoDiscordDroppedPins } = await import('$lib/server/demo-data');
    rememberDroppedPins(ctx.cookies, ctx.guildId, droppedPinNotice(demoDiscordDroppedPins()));
  }
  return { ok: true };
}

async function demoSave(ctx: ActionCtx) {
  const { demoDiscordConfig, demoDiscordRefusedField } = await import('$lib/server/demo-data');
  const stored = demoDiscordConfig().config;
  const draft = parseDraft(ctx.form.get('config'));
  const { errors } = mergeDiscordConfig(stored, draft);
  const refused = demoDiscordRefusedField();
  const fields = errors.map((e) => String(e.field));
  if (draft[refused] !== stored[refused]) fields.push(refused);
  if (fields.length === 0) return { ok: true };
  return fail(400, {
    ok: false,
    code: 'invalid',
    error: `Some settings were not valid: ${fields.join(', ')}.`,
    fields
  });
}

// The config row is load-bearing (it is the draft the page edits), so a
// failure there degrades the page. Layout and status are decorative: the
// pickers fall back to a disabled control and the status card to an offline
// pill, so a blip in either must not take the page with it.
type GuildReads = {
  config: DiscordConfig;
  version: number;
  found: boolean;
  layout: DiscordLayout;
  status: DiscordStatus;
  degraded: boolean;
};

async function guildReads(target: DiscordGuildTarget): Promise<GuildReads> {
  const [row, layout, status] = await Promise.all([
    readGuildConfig(target).catch((e) => {
      logger.warn({ err: e }, '[discord] guild config unavailable');
      return null;
    }),
    loadLayout(target),
    loadStatus(target)
  ]);
  if (!row) {
    return {
      config: { ...blankDiscordConfig(), guildId: target.guildId },
      version: 0,
      // `found` false plus `degraded` true is "we do not know", which must
      // never be read as "this guild has no config" -- that is the state the
      // legacy migration refuses to act on.
      found: false,
      layout,
      status,
      degraded: true
    };
  }
  return { config: row.config, version: row.version, found: row.found, layout, status, degraded: false };
}

async function loadLayout(target: DiscordGuildTarget): Promise<DiscordLayout> {
  try {
    return await guildLayout(target);
  } catch (e) {
    logger.warn({ err: e }, '[discord] layout unavailable');
    return blankLayout();
  }
}

async function loadStatus(target: DiscordGuildTarget): Promise<DiscordStatus> {
  try {
    return await botStatus(target);
  } catch (e) {
    logger.warn({ err: e }, '[discord] status unavailable');
    return { ...blankStatus(), code: 'discord_unavailable' };
  }
}

type ActionCtx = {
  uid: string;
  guildId: string;
  target: DiscordGuildTarget;
  session: Session | null | undefined;
  locals: App.Locals;
  cookies: Cookies;
  form: FormData;
};

async function actionContext({ cookies, request, locals, params }: RequestEvent): Promise<ActionCtx | null> {
  gate(locals.session);
  if (!DEMO && !locals.session) return null;
  const uid = effectiveId(locals.session);
  const guildId = params.guildId ?? '';
  return {
    uid,
    guildId,
    target: { userId: uid, guildId },
    session: locals.session,
    locals,
    cookies,
    form: await request.formData()
  };
}

type Outcome<T> = { ok: true; data: T } | { ok: false; error: string };

type ActionWork = { label: string; failMsg: string };

async function attempt<T>(work: ActionWork, run: () => Promise<T>): Promise<Outcome<T>> {
  try {
    return { ok: true, data: await run() };
  } catch (e) {
    if (isRedirect(e)) throw e;
    logger.error({ err: e }, `[discord] ${work.label} failed`);
    return { ok: false, error: work.failMsg };
  }
}

// A soft refusal: the call reached outgress and it said no. `code` is what the
// page switches on; `error` is the sentence an older outgress sent, kept only
// as a last resort. `fields` names the controls a validation refusal was
// about, so the page can say which pick did not take.
type Refusal = { error: string; code?: string; fields?: string[] };

function refusalOf(data: Record<string, unknown>): Refusal | null {
  const error = typeof data.error === 'string' ? data.error : '';
  const code = typeof data.code === 'string' ? data.code : '';
  if (!error && !code) return null;
  const fields = Array.isArray(data.fields) ? (data.fields as string[]) : undefined;
  return { error, code, ...(fields ? { fields } : {}) };
}

// Ownership again, per action. A page that rendered before a guild was
// disconnected still posts to this URL, and the load's check does not run for
// a form POST.
async function assertOwned(ctx: ActionCtx): Promise<void> {
  if (DEMO) return;
  ownedGuild(await listGuilds({ userId: ctx.uid }), ctx.guildId);
}

function discordAction<T extends Record<string, unknown>>(
  work: ActionWork,
  run: (ctx: ActionCtx) => Promise<T | Refusal>
) {
  return async (event: RequestEvent) => {
    const ctx = await actionContext(event);
    if (!ctx) return fail(401, { ok: false, error: 'Not signed in.' });
    // The page is reachable while locked so it can explain itself; its writes
    // are not. Without this, a stale form on a downgraded board would still
    // save.
    if (!(await assertModuleUnlocked(event.locals, DISCORD_DEF))) {
      return fail(403, { ok: false, code: 'locked', error: 'Discord is in beta and open to Premium channels only.' });
    }
    if (DEMO) return demoOutcome(work.label, ctx);
    const r = await attempt(work, async () => {
      await assertOwned(ctx);
      return run(ctx);
    });
    if (!r.ok) return fail(400, { ok: false, error: r.error });
    const refusal = refusalOf(r.data);
    if (refusal) return fail(400, { ok: false, ...refusal });
    return { ok: true, ...r.data };
  };
}

/**
 * The whole draft arrives as one hidden JSON field.
 *
 * The old form posted a `name` per input plus a hidden mirror per switch,
 * which meant the tri-state flags had three sources of truth and a control the
 * page chose not to render silently cleared its field. One field means the
 * page's own state object IS the payload, and every rule lives in the shared
 * merge.
 */
function parseDraft(raw: FormDataEntryValue | null): Record<string, unknown> {
  if (typeof raw !== 'string') return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed === null) return {};
    if (typeof parsed !== 'object') return {};
    if (Array.isArray(parsed)) return {};
    return parsed as Record<string, unknown>;
  } catch {
    return {};
  }
}

/** The version the form was rendered against. A missing or unparseable one
 *  becomes 0, which outgress treats as "never read" and refuses on an existing
 *  row rather than clobbering it. */
function parseVersion(raw: FormDataEntryValue | null): number {
  if (typeof raw !== 'string') return 0;
  const n = Number.parseInt(raw.trim(), 10);
  return Number.isSafeInteger(n) && n >= 0 ? n : 0;
}

function fieldNames(errors: { field: string }[]): string {
  return errors.map((e) => e.field).join(', ');
}

export const guildActions: Actions = {
  save: discordAction({ label: 'save', failMsg: 'Could not save Discord settings.' }, async (ctx) => {
    const row = await readGuildConfig(ctx.target);
    const { config, errors } = mergeDiscordConfig(row.config, parseDraft(ctx.form.get('config')));
    // Persist FIRST, refuse second. `mergeDiscordConfig`'s contract is that a
    // rejected field keeps its stored value and every accepted one is applied,
    // so the merged config is always safe to write -- and returning the
    // refusal before writing threw away the twenty good changes in the same
    // draft to punish the one bad one, which is what a streamer reads as "the
    // save button does nothing".
    const result = await saveGuildConfig({
      ...ctx.target,
      config,
      expectedVersion: parseVersion(ctx.form.get('version'))
    });
    if (result.error || result.code) return { error: result.error, code: result.code };
    auditDashboardImpersonation(ctx.session, 'discord:save', ctx.guildId);
    if (errors.length) {
      return { error: `Some settings were not valid: ${fieldNames(errors)}.`, code: 'invalid', fields: errors.map((e) => e.field) };
    }
    return { version: result.version };
  }),

  setup: discordAction({ label: 'setup', failMsg: 'Could not set up this server.' }, async (ctx) => {
    requireDiscordActor(ctx.locals);
    const view = await readDiscord({ userId: ctx.uid });
    const row = await readGuildConfig(ctx.target);
    const login = ctx.session?.login ?? view.twitchLogin ?? row.config.twitchLogin;
    const result = await setupGuild(
      {
        ...ctx.target,
        // Who pressed the button, which is ctx.uid's own id unless a staff
        // member is impersonating; the binding records the actor, not the
        // account being acted on.
        installedBy: ctx.session?.user_id ?? ctx.uid,
        // The saved toggle decides whether the fill creates the subscriber
        // tier, so setup reflects what the streamer chose rather than always
        // building a locked category they may never use.
        subscribers: alertOff(row.config.subscribersEnabled),
        // Pins win over name lookup, so a streamer who already has a Mods role
        // keeps it instead of getting a second one.
        pinnedRoles: pinnedRolesOf(row.config)
      },
      { ...row.config, twitchLogin: login }
    );
    if (result.error) return { error: result.error, code: result.code };
    // A pin whose role is gone is cleared here rather than left for the
    // streamer to notice: the fill already created a replacement, so keeping
    // the dead id would point the NEXT setup at it again and keep the picker
    // showing "Pinned" over a role Discord has forgotten.
    const config = clearPinnedSlots({ ...result.config, twitchLogin: login }, result.droppedPins);
    const saved = await persistSetup(ctx.target, config);
    if (saved.error || saved.code) return { error: saved.error, code: saved.code };
    auditDashboardImpersonation(ctx.session, 'discord:setup', ctx.guildId);
    rememberDroppedPins(ctx.cookies, ctx.guildId, result.droppedPins);
    return { refused: result.refused };
  }),

  repost: discordAction({ label: 'repost', failMsg: 'Could not repost the ticket panel.' }, async (ctx) => {
    const row = await readGuildConfig(ctx.target);
    if (!alertOn(row.config.ticketsEnabled)) {
      return { error: 'Turn the ticket desk on first.', code: 'tickets_off' };
    }
    const result = await repostDesk(ctx.target, row.config);
    if (result.error) return { error: result.error, code: result.code };
    auditDashboardImpersonation(ctx.session, 'discord:repost', ctx.guildId);
    return { messageId: result.messageId };
  }),

  // Scoped to this guild: the other servers this broadcaster owns keep their
  // bindings and their rows, and the master switch is not touched.
  disconnect: async (event: RequestEvent) => {
    const run = discordAction({ label: 'disconnect', failMsg: 'Could not disconnect Discord.' }, async (ctx) => {
      await unbindGuild(ctx.target);
      auditDashboardImpersonation(ctx.session, 'discord:disconnect', ctx.guildId);
      return {};
    });
    const result = await run(event);
    if ('ok' in result && result.ok === true) throw redirect(303, '/discord');
    return result;
  }
};
