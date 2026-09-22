// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The custom-command Variable inventory: the union of
// web/marketing/src/i18n/builder.ts's DYNAMIC/UTILITIES/VIEWER/MODULE_FACTS/
// CHAT_ROOM/CHANNEL_FACTS/EMOTES arrays and its `id: 'custom'` surface vars,
// web/dashboard/src/lib/components/commands/ResponseEditor.svelte's
// DEFAULT_TOKENS, and the resolver inventory in
// app/twitch/sesame/engine/scope/token_catalog.go (mirrored at
// app/twitch/sesame/engine/scope/testdata/token_catalog.golden.json, which
// ./parity.test.ts checks this list against).
//
// Order: ResponseEditor's DEFAULT_TOKENS order first (today's chip order, so
// a caller migrating off that array keeps the same first impression), then
// the entries DEFAULT_TOKENS lacks beside their siblings (the two emote
// providers it folds into one chip, the two song subfields) and urlfetch last.
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
  SEVENTV_EMOTES_SAMPLE,
  SONG_ARTIST_SAMPLE,
  SONG_SAMPLE,
  SONG_TITLE_SAMPLE,
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
  { id: 'user', head: 'user', category: 'basics', aliases: ['sender'], forms: [{ syntax: '{user}', example: '{user}', output: USER_SAMPLE }] },
  { id: 'touser', head: 'touser', category: 'basics', aliases: ['target'], forms: [{ syntax: '{touser}', example: '{touser}', output: TOUSER_SAMPLE }] },
  { id: 'args', head: 'args', category: 'basics', forms: [{ syntax: '{args}', example: '{args}', output: ARGS_SAMPLE }] },
  {
    id: 'positional',
    head: 'positional',
    category: 'arguments',
    forms: [
      { syntax: '{1}', example: '{1}', output: POSITIONAL_WORD_SAMPLE },
      { syntax: '{2:}', example: '{2:}', output: POSITIONAL_REST_SAMPLE, chipHint: 'restHint' },
      // {:m} and {n:m}: guide-only, like every form beside the first two —
      // only {2:} gets its own chip (see VariableForm.chipHint).
      { syntax: '{:2}', example: '{:2}', output: POSITIONAL_LEADING_SLICE_SAMPLE },
      { syntax: '{2:4}', example: '{2:4}', output: POSITIONAL_BOUNDED_SLICE_SAMPLE }
    ]
  },
  { id: 'channel', head: 'channel', category: 'basics', forms: [{ syntax: '{channel}', example: '{channel}', output: CHANNEL_SAMPLE }] },
  { id: 'userid', head: 'user.id', category: 'basics', aliases: ['userid'], forms: [{ syntax: '{user.id}', example: '{user.id}', output: USERID_SAMPLE }] },
  { id: 'userLogin', head: 'user.login', category: 'basics', forms: [{ syntax: '{user.login}', example: '{user.login}', output: USER_LOGIN_SAMPLE }] },
  { id: 'command', head: 'command', category: 'basics', forms: [{ syntax: '{command}', example: '{command}', output: COMMAND_SAMPLE }] },
  { id: 'counter', head: 'counter', category: 'counters', forms: [{ syntax: '{counter:<name>}', example: '{counter:falls}', output: COUNTER_SAMPLE }], requires: 'loyalty' },
  {
    id: 'random',
    head: 'random',
    category: 'dynamic',
    forms: [
      { syntax: '{random}', example: '{random}', output: RANDOM_SAMPLE },
      { syntax: '{random:<min>-<max>}', example: '{random:1-6}', output: RANDOM_RANGE_SAMPLE }
    ]
  },
  { id: 'choice', head: 'choice', category: 'dynamic', forms: [{ syntax: '{choice:<a>,<b>,…}', example: '{choice:yes,no,maybe}', output: CHOICE_SAMPLE }] },
  { id: 'math', head: 'math', category: 'utilities', forms: [{ syntax: '{math:<expr>}', example: '{math:1+2*3}', output: '7' }] },
  { id: 'querystring', head: 'querystring', category: 'utilities', forms: [{ syntax: '{querystring}', example: '{querystring}', output: QUERYSTRING_SAMPLE }] },
  { id: 'queryescape', head: 'queryescape', category: 'utilities', forms: [{ syntax: '{queryescape:<text>}', example: '{queryescape:hello world}', output: 'hello+world' }] },
  { id: 'pathescape', head: 'pathescape', category: 'utilities', forms: [{ syntax: '{pathescape:<text>}', example: '{pathescape:hello world}', output: 'hello%20world' }] },
  { id: 'repeat', head: 'repeat', category: 'utilities', forms: [{ syntax: '{repeat:<n>:<text>}', example: '{repeat:3:bagel}', output: 'bagel bagel bagel' }] },
  { id: 'countdown', head: 'countdown', category: 'utilities', forms: [{ syntax: '{countdown:<date>}', example: '{countdown:2026-12-25}', output: COUNTDOWN_SAMPLE }] },
  { id: 'countup', head: 'countup', category: 'utilities', forms: [{ syntax: '{countup:<date>}', example: '{countup:2020-01-01}', output: COUNTDOWN_SAMPLE }] },
  // One form, deliberately: the two-branch shape with a plain name. Every
  // other shape ({if:name=value:…:…}, a one-branch form, a cond carrying its
  // own payload) is a small edit away from this one, and a chip surface only
  // ever shows forms[0] as the literal it inserts (docs/specs/
  // variables-catalog.md D5), so a second form here would just be a form
  // nothing renders. Moved from ResponseEditor.svelte's old DEFAULT_TOKENS,
  // which explained the same choice next to the hand-written chip.
  { id: 'if', head: 'if', category: 'utilities', forms: [{ syntax: '{if:<name>:<then>:<else>}', example: '{if:touser:hi there:hi everyone}', output: IF_SAMPLE }] },
  {
    id: 'followage',
    head: 'followage',
    category: 'viewer',
    requires: 'followage',
    forms: [
      { syntax: '{followage}', example: '{followage}', output: FOLLOWAGE_SAMPLE },
      { syntax: '{followage:<login>}', example: '{followage:alex}', output: FOLLOWAGE_SAMPLE }
    ]
  },
  {
    id: 'accountage',
    head: 'accountage',
    category: 'viewer',
    requires: 'accountage',
    forms: [
      { syntax: '{accountage}', example: '{accountage}', output: ACCOUNTAGE_SAMPLE },
      { syntax: '{accountage:<login>}', example: '{accountage:alex}', output: ACCOUNTAGE_SAMPLE }
    ]
  },
  {
    id: 'points',
    head: 'points',
    category: 'viewer',
    requires: 'loyalty',
    forms: [
      { syntax: '{points}', example: '{points}', output: POINTS_SAMPLE },
      { syntax: '{points:<login>}', example: '{points:alex}', output: POINTS_SAMPLE }
    ]
  },
  { id: 'pointsname', head: 'points.name', category: 'viewer', requires: 'loyalty', aliases: ['pointsname'], forms: [{ syntax: '{points.name}', example: '{points.name}', output: POINTS_NAME_SAMPLE }] },
  {
    id: 'watchtime',
    head: 'watchtime',
    category: 'viewer',
    requires: 'loyalty',
    forms: [
      { syntax: '{watchtime}', example: '{watchtime}', output: WATCHTIME_SAMPLE },
      { syntax: '{watchtime:<login>}', example: '{watchtime:alex}', output: WATCHTIME_SAMPLE }
    ]
  },
  { id: 'count', head: 'count', category: 'counters', requires: 'loyalty', forms: [{ syntax: '{count:<name>}', example: '{count:deaths}', output: COUNTER_SAMPLE }] },
  { id: 'uses', head: 'uses', category: 'counters', forms: [{ syntax: '{uses}', example: '{uses}', output: USES_SAMPLE }] },
  {
    id: 'quote',
    head: 'quote',
    category: 'utilities',
    requires: 'quotes',
    forms: [
      { syntax: '{quote}', example: '{quote}', output: QUOTE_SAMPLE },
      { syntax: '{quote:<number>}', example: '{quote:12}', output: QUOTE_SAMPLE }
    ]
  },
  { id: 'time', head: 'time', category: 'utilities', requires: 'time', forms: [{ syntax: '{time}', example: '{time}', output: TIME_SAMPLE }] },
  { id: 'song', head: 'song', category: 'queue', requires: 'songqueue', forms: [{ syntax: '{song}', example: '{song}', output: SONG_SAMPLE }] },
  { id: 'songTitle', head: 'song.title', category: 'queue', requires: 'songqueue', forms: [{ syntax: '{song.title}', example: '{song.title}', output: SONG_TITLE_SAMPLE }] },
  { id: 'songArtist', head: 'song.artist', category: 'queue', requires: 'songqueue', forms: [{ syntax: '{song.artist}', example: '{song.artist}', output: SONG_ARTIST_SAMPLE }] },
  { id: 'chatters', head: 'chatters', category: 'chat', forms: [{ syntax: '{chatters}', example: '{chatters}', output: CHATTERS_SAMPLE }] },
  { id: 'randomChatter', head: 'random.chatter', category: 'chat', forms: [{ syntax: '{random.chatter}', example: '{random.chatter}', output: RANDOM_CHATTER_SAMPLE }] },
  {
    id: 'emotes',
    head: 'emotes',
    category: 'emotes',
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
  { id: 'randomEmote', head: 'random.emote', category: 'emotes', forms: [{ syntax: '{random.emote}', example: '{random.emote}', output: RANDOM_EMOTE_SAMPLE }] },
  {
    id: 'uptime',
    head: 'uptime',
    category: 'channel',
    requires: 'uptime',
    forms: [
      { syntax: '{uptime}', example: '{uptime}', output: UPTIME_SAMPLE },
      { syntax: '{uptime:<channel>}', example: '{uptime:someone}', output: UPTIME_SAMPLE }
    ]
  },
  {
    id: 'title',
    head: 'title',
    category: 'channel',
    requires: 'title',
    forms: [
      { syntax: '{title}', example: '{title}', output: TITLE_SAMPLE },
      { syntax: '{title:<channel>}', example: '{title:someone}', output: TITLE_SAMPLE }
    ]
  },
  {
    id: 'game',
    head: 'game',
    category: 'channel',
    requires: 'game',
    forms: [
      { syntax: '{game}', example: '{game}', output: GAME_SAMPLE },
      { syntax: '{game:<channel>}', example: '{game:someone}', output: GAME_SAMPLE }
    ]
  },
  { id: 'channelViewers', head: 'channel.viewers', category: 'channel', forms: [{ syntax: '{channel.viewers}', example: '{channel.viewers}', output: CHANNEL_VIEWERS_SAMPLE }] },
  // Not on ResponseEditor's chip strip: needs a saved data source first.
  { id: 'urlfetch', head: 'urlfetch', category: 'utilities', forms: [{ syntax: '{urlfetch:<definition>}', example: '{urlfetch:weather}', output: URLFETCH_SAMPLE }] },
];
