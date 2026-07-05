import type { IconName } from './icons';
// Wire types mirroring the Go NATS RPC contracts (JSON over core NATS).
export type Perm = 'everyone' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster';
export type Tier = 'premium' | 'standard';
export type Role = 'streamer' | 'mod';

// Ordered low -> high privilege; drives the access <select> in the dashboard.
export const PERMS: readonly Perm[] = ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'];
export const PERM_LABELS: Record<Perm, string> = {
  everyone: 'Everyone',
  sub: 'Subscribers',
  vip: 'VIPs',
  mod: 'Moderators',
  lead_mod: 'Lead moderators',
  broadcaster: 'Broadcaster'
};

export interface CommandView {
  name: string;
  // Alternate names the command also answers to in chat.
  aliases?: string[];
  response: string;
  is_active: boolean;
  stream_online_only?: boolean;
  perm?: Perm;
  // Cooldown in seconds; 0 or undefined means no cooldown.
  cooldown?: number;
  // Twitch id of the only user allowed to run the command; '' or undefined = unrestricted.
  allowed_user_id?: string;
  // Lifetime execution counter. The backend sends a number; older sample data
  // used human-formatted strings ('1.2k'), so both are accepted for display.
  uses?: number | string;
  // When true this is a built-in command: its behavior is baked into the bot,
  // it has no editable response, and its on/off state is stored in the modules
  // service (not the commands service). The dashboard renders it read-only with
  // a toggle + preview. See BUILTIN_COMMANDS.
  builtin?: boolean;
}

// --- Built-in command catalog --------------------------------------------
// Built-in commands are behaviors baked into the bot (not user text). They show
// on the commands page alongside custom commands, flagged builtin, but they
// cannot be renamed, deleted, or given a custom response — only toggled on/off.
// Their per-user on/off state lives in the modules service under `id` (a missing
// row means defaultActive). Adding one is a row here + the matching sesame
// built-in module. They are deliberately NOT in MODULE_CATALOG (never shown on
// the modules page).

export interface BuiltinCommandDef {
  // id is both the chat trigger and the modules-service key for the toggle.
  id: string;
  label: string;
  // summary is shown in the command row where a custom command shows its
  // response (built-ins have no response).
  summary: string;
  description: string; // longer copy for the inspector
  // usage lists example invocations shown in the inspector.
  usage: string[];
  // preview is the bot REPLY template, rendered through ChatPreview so the
  // inspector shows the same rehearsal as a custom command (viewer line + the
  // ItsBagelBot name/logo). previewArgs is what the viewer types after the
  // trigger; previewSamples fills tokens ChatPreview does not know by default.
  preview: string;
  previewArgs?: string;
  previewSamples?: Record<string, string>;
  defaultActive: boolean;
  defaultPerm: Perm;
  defaultCooldown: number; // seconds
  // liveOnly commands run only while the broadcaster is streaming.
  liveOnly: boolean;
}

export const BUILTIN_COMMANDS: readonly BuiltinCommandDef[] = [
  {
    id: 'clip',
    label: 'Clip',
    summary: 'Built-in · clips the last moments of the stream and posts the link.',
    description:
      'Viewers create a clip of the recent stream and the bot replies in chat with the clip link. Add an optional title after the command. Only works while you are live.',
    usage: ['!clip', '!clip <title>'],
    // Real reply format: "🎬 <clipper> clipped: <title> → <url>" (see
    // app/outgress/internal/worker clipReplyText). {user} = the clipper, {target}
    // = the title argument (standard command token).
    preview: '🎬 {user} clipped: {target} → {clip}',
    previewArgs: 'That is amazing',
    previewSamples: { target: 'That is amazing', clip: 'clips.twitch.tv/AbCdEf' },
    defaultActive: true,
    defaultPerm: 'everyone',
    defaultCooldown: 15,
    liveOnly: true
  }
];

export function builtinDef(id: string): BuiltinCommandDef | undefined {
  return BUILTIN_COMMANDS.find((b) => b.id === id);
}

export const BUILTIN_NAMES: ReadonlySet<string> = new Set(BUILTIN_COMMANDS.map((b) => b.id));

export interface AdminUser {
  user_id: string;
  username: string;
  display_name?: string;
  status?: string;
}

export interface UserStats {
  total_users: number;
  active_users: number;
  premium_users: number;
  vip_users: number;
  paid_users: number;
}

export interface Shard {
  shard_id: number;
  state: string;
  node: string;
  // Worker node (machine) name the shard runs on. Falls back to the host part
  // of `node` when unset (e.g. local dev without the downward-API env).
  host?: string;
  session_id?: string;
  bound: boolean;
  handshake_in_flight?: boolean;
  keepalive_ms?: number;
  attempts?: number;
  load?: number;
}

export interface ShardSnapshot {
  generated_at: string;
  reporter: string;
  nodes: string[];
  shard_count: number;
  conduit_manager?: { state: string; node: string; conduit_id?: string };
  shards: Shard[];
  desired_count: number;
  target: number;
  min_shards: number;
  autoscale: boolean;
}

export interface NavLink {
  href: string;
  icon: IconName;
  label: string;
  active?: boolean;
  locked?: boolean;
  count?: string | number;
}

export interface NavGroupDef {
  label?: string;
  items: NavLink[];
}

// A dashboard the signed-in user has been granted access to (a delegation
// received). Rendered in the topbar account menu as a quick-switch link into
// the owner's board via /delegate/enter.
export interface DashboardLink {
  // Full href to enter the board, e.g. /delegate/enter?owner=<id>.
  href: string;
  // Owner's Twitch login, shown as the row name + gradient-badge initial.
  name: string;
}

// --- Module catalog -------------------------------------------------------
// The user-facing modules the dashboard lets a broadcaster toggle/configure.
// Core, hidden modules (the command processor, the live tracker, and the system
// module that owns !ping/!itsbagelbot and the bagel greeting) are deliberately
// NOT listed here: they are always on and never shown.

export type ModuleFieldType = 'text' | 'textarea' | 'number' | 'toggle';

export interface ModuleField {
  // key is the JSON property written into the module's Configs blob.
  key: string;
  label: string;
  type: ModuleFieldType;
  placeholder?: string;
  help?: string;
}

// One chat line a module can post, rendered as a row on the module page. Clicking
// the row opens the exact same builder as a custom command's response (the shared
// ResponseEditor + ChatPreview, standard {user}/{target}/… tokens). messageKey/
// enableKey are the Configs JSON keys the matching sesame module reads (see
// app/sesame/modules).
export interface ModuleReply {
  key: string; // stable row id
  label: string; // 'Follow alert'
  tagline: string; // short row description
  // Preview context: what fires this line, e.g. 'on follow'.
  event: string;
  messageKey: string; // Configs key holding the template
  // Configs key for this reply's own on/off toggle; omit when the reply has no
  // per-reply switch (it fires whenever the module is on). Stored "on"/"off";
  // empty/absent means on, matching sesame's alertOn semantics.
  enableKey?: string;
  defaultMessage: string; // sesame default template (placeholder + preview fallback)
}

export interface ModuleDef {
  // id is the ModuleView.name key in the modules service.
  id: string;
  label: string;
  tagline: string; // one-liner for the tile
  description: string; // longer copy for the module page
  icon: IconName;
  defaultEnabled: boolean;
  // The module's configurable chat lines (the "commands" of the module page).
  replies: ModuleReply[];
  // Plain non-reply settings (rendered in the settings strip). Optional; the
  // current modules have none beyond their master enable + per-reply toggles.
  settings?: ModuleField[];
}

// A module's current state as shown on the dashboard: catalog metadata merged
// with the broadcaster's stored row.
export interface ModuleState {
  def: ModuleDef;
  enabled: boolean;
  config: Record<string, string>;
}

export const MODULE_CATALOG: readonly ModuleDef[] = [
  {
    id: 'shoutout',
    label: 'Auto Shoutout',
    tagline: 'Welcome incoming raids with an automatic shoutout.',
    description:
      'When another channel raids in, the bot posts a shoutout pointing your chat at the raider. Turn the module on and customize the shoutout line.',
    icon: 'send',
    defaultEnabled: false,
    replies: [
      {
        key: 'shoutout',
        label: 'Raid shoutout',
        tagline: 'Automated chat shoutout when raided',
        event: 'on raid',
        messageKey: 'message',
        defaultMessage:
          'Massive shoutout to {raider} for the raid with {viewers} viewers! Check them out at twitch.tv/{raider.login}'
      }
    ]
  },
  {
    id: 'alerts',
    label: 'Chat Alerts',
    tagline: 'Announce follows, subs, cheers and raids in chat.',
    description:
      'The bot posts a chat line when someone follows, subscribes, cheers, or raids. Turn each alert on or off and customize its message. New alerts default on.',
    icon: 'bell',
    defaultEnabled: true,
    replies: [
      {
        key: 'follow',
        label: 'Follow alert',
        tagline: 'When someone follows your channel.',
        event: 'on follow',
        enableKey: 'followEnabled',
        messageKey: 'followMessage',
        defaultMessage: 'Thank you for following the channel, {user}!'
      },
      {
        key: 'sub',
        label: 'Subscribe alert',
        tagline: 'When someone subscribes.',
        event: 'on subscribe',
        enableKey: 'subEnabled',
        messageKey: 'subMessage',
        defaultMessage: 'Welcome to the community, {user}! Thank you for subscribing!'
      },
      {
        key: 'cheer',
        label: 'Cheer alert',
        tagline: 'When someone cheers bits.',
        event: 'on cheer',
        enableKey: 'cheerEnabled',
        messageKey: 'cheerMessage',
        defaultMessage: 'Thank you for the {bits} bits, {user}!'
      },
      {
        key: 'raid',
        label: 'Raid alert',
        tagline: 'When another channel raids in.',
        event: 'on raid',
        enableKey: 'raidEnabled',
        messageKey: 'raidMessage',
        defaultMessage: '{user} is raiding the channel with {viewers} viewers! Welcome everyone!'
      }
    ]
  }
];

export function moduleDef(id: string): ModuleDef | undefined {
  return MODULE_CATALOG.find((m) => m.id === id);
}
