// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The custom-command Variable inventory: the union of
// web/marketing/src/i18n/builder.ts's DYNAMIC/UTILITIES/VIEWER/MODULE_FACTS/
// CHAT_ROOM/CHANNEL_FACTS/EMOTES arrays and its `id: 'custom'` surface vars,
// the chip strip web/dashboard/src/lib/components/commands/ResponseEditor.svelte
// used to hand-keep as its own DEFAULT_TOKENS array (deleted once
// ResponseEditor started rendering VariablePalette, which reads this file
// through surfaces.ts's pinnedFor/sheetFor/chipsFor instead), and the resolver
// inventory in app/twitch/sesame/engine/scope/token_catalog.go (mirrored at
// app/twitch/sesame/engine/scope/testdata/token_catalog.golden.json, which
// ./parity.test.ts checks this list against).
//
// Order: the old DEFAULT_TOKENS order first (that was the chip order every
// broadcaster already knew, so the migration off it kept the same first
// impression), then the entries it lacked beside their siblings (the two
// emote providers it folds into one chip, the two song subfields) and
// urlfetch last.
//
// Copy (name/hint/desc) is not here: it lives in the i18n locales under
// vars.<id>, read through @bagel/kit/i18n. This file is structure only.

import {
  ARGS_SAMPLE,
  BTTV_EMOTES_SAMPLE,
  CHANNEL_SAMPLE,
  CHANNEL_VIEWERS_SAMPLE,
  CHATTERS_SAMPLE,
  CHOICE_SAMPLE,
  COMMAND_SAMPLE,
  COUNTDOWN_SAMPLE,
  COUNTER_SAMPLE,
  FFZ_EMOTES_SAMPLE,
  FOLLOWAGE_SAMPLE,
  FOLLOWERS_SAMPLE,
  ACCOUNTAGE_SAMPLE,
  GAME_SAMPLE,
  IF_SAMPLE,
  POINTS_NAME_SAMPLE,
  POINTS_SAMPLE,
  POSITIONAL_BOUNDED_SLICE_SAMPLE,
  POSITIONAL_LEADING_SLICE_SAMPLE,
  POSITIONAL_REST_SAMPLE,
  POSITIONAL_WORD_SAMPLE,
  QUERYSTRING_SAMPLE,
  QUOTE_SAMPLE,
  RANDOM_CHATTER_SAMPLE,
  RANDOM_EMOTE_SAMPLE,
  RANDOM_RANGE_SAMPLE,
  RANDOM_SAMPLE,
  RANDOM_VIEWER_SAMPLE,
  SEVENTV_EMOTES_SAMPLE,
  SONG_ARTIST_SAMPLE,
  SONG_SAMPLE,
  SONG_TITLE_SAMPLE,
  SUBS_SAMPLE,
  TIME_PLACE_SAMPLE,
  TIME_SAMPLE,
  TITLE_SAMPLE,
  TOUSER_SAMPLE,
  UPTIME_SAMPLE,
  URLFETCH_SAMPLE,
  USER_LOGIN_SAMPLE,
  USER_SAMPLE,
  USERID_SAMPLE,
  USES_SAMPLE,
  WATCHTIME_SAMPLE
} from './preview-values';
import type { VariableDef } from './types';

export const VARIABLES: readonly VariableDef[] = [
  { id: 'user', head: 'user', group: 'who', requires: null, pinned: true, aliases: ['sender'], forms: [{ syntax: '{user}', example: '{user}', output: USER_SAMPLE }] },
  { id: 'touser', head: 'touser', group: 'who', requires: null, pinned: true, aliases: ['target'], forms: [{ syntax: '{touser}', example: '{touser}', output: TOUSER_SAMPLE }] },
  { id: 'args', head: 'args', group: 'typed', requires: null, pinned: true, forms: [{ syntax: '{args}', example: '{args}', output: ARGS_SAMPLE }] },
  {
    id: 'positional',
    head: 'positional',
    group: 'typed',
    requires: null,
    forms: [
      { syntax: '{1}', example: '{1}', output: POSITIONAL_WORD_SAMPLE },
      { syntax: '{2:}', example: '{2:}', output: POSITIONAL_REST_SAMPLE, chipHint: 'restHint' },
      // Every form beside the canonical {1} carries chipHint (see
      // VariableForm.chipHint): {2:}, {:2} and {2:4} are the three shapes a
      // broadcaster reaches for often enough to want their own chip, not
      // just a guide-page mention.
      { syntax: '{:2}', example: '{:2}', output: POSITIONAL_LEADING_SLICE_SAMPLE, chipHint: 'leadingHint' },
      { syntax: '{2:4}', example: '{2:4}', output: POSITIONAL_BOUNDED_SLICE_SAMPLE, chipHint: 'boundedHint' }
    ]
  },
  { id: 'channel', head: 'channel', group: 'stream', requires: null, forms: [{ syntax: '{channel}', example: '{channel}', output: CHANNEL_SAMPLE }] },
  { id: 'userid', head: 'user.id', group: 'who', requires: null, aliases: ['userid'], forms: [{ syntax: '{user.id}', example: '{user.id}', output: USERID_SAMPLE }] },
  { id: 'userLogin', head: 'user.login', group: 'who', requires: null, forms: [{ syntax: '{user.login}', example: '{user.login}', output: USER_LOGIN_SAMPLE }] },
  { id: 'command', head: 'command', group: 'who', requires: null, forms: [{ syntax: '{command}', example: '{command}', output: COMMAND_SAMPLE }] },
  {
    id: 'counter',
    head: 'counter',
    group: 'data',
    requires: 'loyalty',
    // Read-only: {counter:x} stopped bumping when the write moved to the
    // command's own "bump a counter" option (docs/specs decision, see
    // app/db/commands/ent/schema/commands.go's bump_counter field). {count:…}
    // is the same read under Go's Aliases (TokenFamily.Aliases,
    // token_catalog.go); it is deliberately not a manifest alias here — the
    // public catalogue teaches one canonical spelling, matching the comment
    // on that Go field.
    forms: [
      { syntax: '{counter:<name>}', example: '{counter:falls}', output: COUNTER_SAMPLE },
      { syntax: '{counter:target:<name>}', example: '{counter:target:falls}', output: COUNTER_SAMPLE, chipHint: 'targetHint' }
    ]
  },
  {
    id: 'random',
    head: 'random',
    group: 'fun',
    requires: null,
    pinned: true,
    forms: [
      { syntax: '{random}', example: '{random}', output: RANDOM_SAMPLE },
      { syntax: '{random:<min>-<max>}', example: '{random:1-6}', output: RANDOM_RANGE_SAMPLE, chipHint: 'rangeHint' }
    ]
  },
  { id: 'choice', head: 'choice', group: 'fun', requires: null, forms: [{ syntax: '{choice:<a>,<b>,…}', example: '{choice:yes,no,maybe}', output: CHOICE_SAMPLE }] },
  { id: 'math', head: 'math', group: 'fun', requires: null, forms: [{ syntax: '{math:<expr>}', example: '{math:1+2*3}', output: '7' }] },
  { id: 'querystring', head: 'querystring', group: 'typed', requires: null, forms: [{ syntax: '{querystring}', example: '{querystring}', output: QUERYSTRING_SAMPLE }] },
  { id: 'queryescape', head: 'queryescape', group: 'typed', requires: null, forms: [{ syntax: '{queryescape:<text>}', example: '{queryescape:hello world}', output: 'hello+world' }] },
  { id: 'pathescape', head: 'pathescape', group: 'typed', requires: null, forms: [{ syntax: '{pathescape:<text>}', example: '{pathescape:hello world}', output: 'hello%20world' }] },
  { id: 'repeat', head: 'repeat', group: 'fun', requires: null, forms: [{ syntax: '{repeat:<n>:<text>}', example: '{repeat:3:bagel}', output: 'bagel bagel bagel' }] },
  { id: 'countdown', head: 'countdown', group: 'fun', requires: null, forms: [{ syntax: '{countdown:<date>}', example: '{countdown:2026-12-25}', output: COUNTDOWN_SAMPLE }] },
  { id: 'countup', head: 'countup', group: 'fun', requires: null, forms: [{ syntax: '{countup:<date>}', example: '{countup:2020-01-01}', output: COUNTDOWN_SAMPLE }] },
  // One form, deliberately: the two-branch shape with a plain name. Every
  // other shape ({if:name=value:…:…}, a one-branch form, a cond carrying its
  // own payload) is a small edit away from this one, and a chip surface only
  // ever shows forms[0] as the literal it inserts (docs/specs/
  // variables-catalog.md D5), so a second form here would just be a form
  // nothing renders. Moved from ResponseEditor.svelte's old DEFAULT_TOKENS,
  // which explained the same choice next to the hand-written chip.
  { id: 'if', head: 'if', group: 'fun', requires: null, pinned: true, forms: [{ syntax: '{if:<name>:<then>:<else>}', example: '{if:touser:hi there:hi everyone}', output: IF_SAMPLE }] },
  {
    id: 'followage',
    head: 'followage',
    group: 'stream',
    requires: 'followage',
    forms: [
      { syntax: '{followage}', example: '{followage}', output: FOLLOWAGE_SAMPLE },
      { syntax: '{followage:<login>}', example: '{followage:alex}', output: FOLLOWAGE_SAMPLE }
    ]
  },
  {
    id: 'accountage',
    head: 'accountage',
    group: 'stream',
    requires: 'accountage',
    forms: [
      { syntax: '{accountage}', example: '{accountage}', output: ACCOUNTAGE_SAMPLE },
      { syntax: '{accountage:<login>}', example: '{accountage:alex}', output: ACCOUNTAGE_SAMPLE }
    ]
  },
  {
    id: 'points',
    head: 'points',
    group: 'data',
    requires: 'loyalty',
    forms: [
      { syntax: '{points}', example: '{points}', output: POINTS_SAMPLE },
      { syntax: '{points:<login>}', example: '{points:alex}', output: POINTS_SAMPLE }
    ]
  },
  { id: 'pointsname', head: 'points.name', group: 'data', requires: 'loyalty', aliases: ['pointsname'], forms: [{ syntax: '{points.name}', example: '{points.name}', output: POINTS_NAME_SAMPLE }] },
  {
    id: 'watchtime',
    head: 'watchtime',
    group: 'data',
    requires: 'loyalty',
    forms: [
      { syntax: '{watchtime}', example: '{watchtime}', output: WATCHTIME_SAMPLE },
      { syntax: '{watchtime:<login>}', example: '{watchtime:alex}', output: WATCHTIME_SAMPLE }
    ]
  },
  // head is 'count', not 'uses': {count} (no payload) is the canonical
  // spelling (scope/uses.go), {uses} its alias. A bare {count} used to be
  // unclaimed; it is now this Variable's own head, which is what let the
  // separate 'count' id (the {count:<name>} counter-read alias) go away —
  // that spelling still resolves (Go's TokenFamily.Aliases for the store
  // family), it just is not taught as its own manifest entry (see 'counter'
  // above).
  { id: 'uses', head: 'count', group: 'data', requires: null, aliases: ['uses'], forms: [{ syntax: '{count}', example: '{count}', output: USES_SAMPLE }] },
  {
    id: 'quote',
    head: 'quote',
    group: 'data',
    requires: 'quotes',
    forms: [
      { syntax: '{quote}', example: '{quote}', output: QUOTE_SAMPLE },
      { syntax: '{quote:<number>}', example: '{quote:12}', output: QUOTE_SAMPLE }
    ]
  },
  {
    id: 'time',
    head: 'time',
    group: 'stream',
    requires: 'time',
    forms: [
      { syntax: '{time}', example: '{time}', output: TIME_SAMPLE },
      // Unlike the bare form, a payload needs no Local Time enrollment: it
      // answers on any channel (docs/specs decision record on {time:<place>}
      // — see app/twitch/sesame/engine/scope/modules.go's Places), so it is
      // not gated by `requires` the way the bare form's chip is.
      { syntax: '{time:<place>}', example: '{time:Paris}', output: TIME_PLACE_SAMPLE, chipHint: 'placeHint' }
    ]
  },
  { id: 'song', head: 'song', group: 'data', requires: 'songqueue', forms: [{ syntax: '{song}', example: '{song}', output: SONG_SAMPLE }] },
  { id: 'songTitle', head: 'song.title', group: 'data', requires: 'songqueue', forms: [{ syntax: '{song.title}', example: '{song.title}', output: SONG_TITLE_SAMPLE }] },
  { id: 'songArtist', head: 'song.artist', group: 'data', requires: 'songqueue', forms: [{ syntax: '{song.artist}', example: '{song.artist}', output: SONG_ARTIST_SAMPLE }] },
  { id: 'chatters', head: 'chatters', group: 'stream', requires: null, forms: [{ syntax: '{chatters}', example: '{chatters}', output: CHATTERS_SAMPLE }] },
  { id: 'randomChatter', head: 'random.chatter', group: 'who', requires: null, forms: [{ syntax: '{random.chatter}', example: '{random.chatter}', output: RANDOM_CHATTER_SAMPLE }] },
  // Distinct from random.chatter beside it: who Twitch reports as connected
  // to chat right now, not who has spoken recently. Neither is gated by a
  // module (see app/twitch/sesame/engine/scope/chatters.go), so this stays
  // ungated like its sibling.
  { id: 'randomViewer', head: 'random.viewer', group: 'who', requires: null, forms: [{ syntax: '{random.viewer}', example: '{random.viewer}', output: RANDOM_VIEWER_SAMPLE }] },
  {
    id: 'emotes',
    head: 'emotes',
    group: 'fun',
    requires: null,
    // One head, three payload forms (the provider), the way {title}/{game}
    // take a channel: forms[0] is still what a chip inserts, the other two
    // are guide-only. 7tvemotes/bttvemotes/ffzemotes were the pre-
    // simplification bare spellings; scope.Emotes keeps them resolving as
    // silent aliases, but they are not modelled here (nothing needs to claim
    // them for parity — rule A only requires the golden's own canonical
    // examples to be covered).
    forms: [
      { syntax: '{emotes:7tv}', example: '{emotes:7tv}', output: SEVENTV_EMOTES_SAMPLE },
      { syntax: '{emotes:bttv}', example: '{emotes:bttv}', output: BTTV_EMOTES_SAMPLE },
      { syntax: '{emotes:ffz}', example: '{emotes:ffz}', output: FFZ_EMOTES_SAMPLE }
    ]
  },
  { id: 'randomEmote', head: 'random.emote', group: 'fun', requires: null, forms: [{ syntax: '{random.emote}', example: '{random.emote}', output: RANDOM_EMOTE_SAMPLE }] },
  {
    id: 'uptime',
    head: 'uptime',
    group: 'stream',
    requires: 'uptime',
    pinned: true,
    forms: [
      { syntax: '{uptime}', example: '{uptime}', output: UPTIME_SAMPLE },
      { syntax: '{uptime:<channel>}', example: '{uptime:someone}', output: UPTIME_SAMPLE }
    ]
  },
  {
    id: 'title',
    head: 'title',
    group: 'stream',
    requires: 'title',
    forms: [
      { syntax: '{title}', example: '{title}', output: TITLE_SAMPLE },
      { syntax: '{title:<channel>}', example: '{title:someone}', output: TITLE_SAMPLE }
    ]
  },
  {
    id: 'game',
    head: 'game',
    group: 'stream',
    requires: 'game',
    forms: [
      { syntax: '{game}', example: '{game}', output: GAME_SAMPLE },
      { syntax: '{game:<channel>}', example: '{game:someone}', output: GAME_SAMPLE }
    ]
  },
  { id: 'channelViewers', head: 'channel.viewers', group: 'stream', requires: null, forms: [{ syntax: '{channel.viewers}', example: '{channel.viewers}', output: CHANNEL_VIEWERS_SAMPLE }] },
  // Neither has a module toggle of its own (Stream Management has no row for
  // either), so — like channel.viewers above — mounting follows the
  // dependency alone; requires stays null.
  { id: 'followers', head: 'followers', group: 'stream', requires: null, forms: [{ syntax: '{followers}', example: '{followers}', output: FOLLOWERS_SAMPLE }] },
  { id: 'subs', head: 'subs', group: 'stream', requires: null, forms: [{ syntax: '{subs}', example: '{subs}', output: SUBS_SAMPLE }] },
  // Not on ResponseEditor's chip strip: needs a saved data source first.
  { id: 'urlfetch', head: 'urlfetch', group: 'data', requires: null, forms: [{ syntax: '{urlfetch:<definition>}', example: '{urlfetch:weather}', output: URLFETCH_SAMPLE }] },
];
