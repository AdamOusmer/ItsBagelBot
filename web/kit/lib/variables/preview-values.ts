// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// One const per sample value, the single home ./engine/rehearsal.ts and
// ./variables.ts both import from (docs/specs/variables-catalog.md D2, D8:
// two copies of the same stand-in is how a rehearsal preview and a guide page
// drift). Moving a value here never changes what it says: every const below
// keeps the exact literal it had in rehearsal.ts.

import { queryEscape } from '../engine/pure';

/** scope.Message's fixed identity fields (messageFields, app/twitch/sesame/
 * engine/scope/message.go): who ran the command, what they typed, who they
 * named, and where. rehearsal.ts's COMMAND_SAMPLES keys onto these. */
export const USER_SAMPLE = 'sesame_sam';
export const ARGS_SAMPLE = 'ferret_king good luck';
export const TOUSER_SAMPLE = 'ferret_king';
export const CHANNEL_SAMPLE = 'bagel_bakery';
export const USERID_SAMPLE = '48291057';
export const USER_LOGIN_SAMPLE = 'sesame_sam';
export const COMMAND_SAMPLE = 'hug';

/** {1} and {2:}: word 1 of ARGS_SAMPLE, and words 2 through the end. Kept as
 * their own consts (rather than a split of ARGS_SAMPLE at use sites) because
 * the positional forms are documentation copy, not a live computation. */
export const POSITIONAL_WORD_SAMPLE = 'ferret_king';
export const POSITIONAL_REST_SAMPLE = 'good luck';

/** {:2} and {2:4}: words 1..2, and words 2..4 (clamped to ARGS_SAMPLE's three
 * words) of the same sample — the {n:m} slice grammar the simplification pass
 * added beside {n} and {n:}. Guide-only (see VariableForm.chipHint), so these
 * never need a chip of their own the way POSITIONAL_REST_SAMPLE does. */
export const POSITIONAL_LEADING_SLICE_SAMPLE = 'ferret_king good';
export const POSITIONAL_BOUNDED_SLICE_SAMPLE = 'good luck';

/** {querystring}: ARGS_SAMPLE, URL-encoded the same way rehearsal.ts's
 * messageScope computes it live (queryEscape(samples.args)), so this never
 * has to be kept in sync by hand. */
export const QUERYSTRING_SAMPLE = queryEscape(ARGS_SAMPLE);

/** {choice:yes,no,maybe}: PURE_SCOPE.choiceSample takes the first
 * comma-separated option, matching app/twitch/sesame/engine/scope/pure.go. */
export const CHOICE_SAMPLE = 'yes';

/** {random:1-6}: randomSample takes the range midpoint, floor((1+6)/2) = 3.
 * web/marketing/src/i18n/builder.ts's own sample for this example says '4',
 * which disagrees with what ./engine/rehearsal.ts actually substitutes;
 * this manifest follows the engine, since that is the value a live preview
 * shows. */
export const RANDOM_RANGE_SAMPLE = '3';

/** {if:touser:hi there:hi everyone}: TOUSER_SAMPLE is non-empty, so the
 * conditional's true branch renders, matching condText in ./engine/tmpl.ts. */
export const IF_SAMPLE = 'hi there';

/** {urlfetch:weather}: scope.External reads a live HTTP response no rehearsal
 * surface can reach while a broadcaster is typing, so this is a plausible
 * stand-in for the guide page rather than a value ./engine/rehearsal.ts
 * substitutes (that scope is not mounted there; see fetch-tokens.ts). */
export const URLFETCH_SAMPLE = '18°C, light rain';

/** Deterministic stand-ins for values the bot rolls or reads at run time, so
 * the rehearsal shows something the bot could produce without re-rolling on
 * every keystroke. */
export const RANDOM_SAMPLE = '57';
export const COUNTER_SAMPLE = '42';

/** {uses} in the preview. Deliberately not a round number and not the counter
 * sample: the two read as the same thing in a template that shows both, and a
 * broadcaster comparing "{counter:hugs} / {uses}" has to be able to see that
 * they are two different numbers. */
export const USES_SAMPLE = '317';

/** {countdown}/{countup} read the wall clock, so the preview shows a fixed,
 * plausible span instead of a live one: a value that ticks while the
 * broadcaster types would redraw the rehearsal on a timer and still not be
 * the value chat sees, since chat sees it whenever the command runs. The
 * wording matches the bot's shared humanizer (the same one !uptime prints
 * through, internal/domain/i18n HumanizeDuration). */
export const COUNTDOWN_SAMPLE = '3 days, 4 hours';

/** Stand-ins for the viewer lookups (scope.Viewer): a follow date, an account
 * creation date and a loyalty standing all live in services the dashboard
 * cannot reach while the broadcaster is typing, so the preview shows a
 * plausible answer rather than a live one. The two spans are worded by the
 * bot's shared humanizer, like every other span it prints. */
export const FOLLOWAGE_SAMPLE = '3 months';
export const ACCOUNTAGE_SAMPLE = '4 years, 2 months';
export const POINTS_SAMPLE = '1280';
export const POINTS_NAME_SAMPLE = 'bagels';
export const WATCHTIME_SAMPLE = '2 hours, 30 minutes';

/** Stand-ins for the module facts (scope.Modules): a saved quote, the
 * broadcaster's local clock and whatever is playing. All three live in
 * services the dashboard cannot reach while a response is being typed, so the
 * preview shows a plausible answer rather than a live one.
 *
 * The quote sample keeps the shape !quote prints (number, text, save date),
 * because that is exactly what the token renders; the clock keeps the 12-hour
 * face, which is the module's default. Chat sees two DIFFERENT quotes for two
 * {quote} spans (they are independent draws): the preview shows the same one
 * twice rather than inventing a second fake quote, because a preview that
 * showed two would suggest the bot knows which two. */
export const QUOTE_SAMPLE = 'Quote #12: bagels are just savoury donuts (2026-01-31)';
export const TIME_SAMPLE = '3:04 PM';

/** {time:<place>}: the payload lookup (tzname.Resolve), which answers on any
 * channel whether or not Local Time is configured — unlike bare {time}, so
 * this is not simply TIME_SAMPLE again. A different clock face from the home
 * sample keeps the two visibly distinct in a preview that shows both. */
export const TIME_PLACE_SAMPLE = '11:04 PM';
export const SONG_TITLE_SAMPLE = 'Everything In Its Right Place';
export const SONG_ARTIST_SAMPLE = 'Radiohead';

/** {song}: MODULE_SAMPLES in rehearsal.ts builds this same string inline
 * (`${SONG_TITLE_SAMPLE} by ${SONG_ARTIST_SAMPLE}`); kept as its own const
 * here so ./variables.ts does not have to repeat the template. */
export const SONG_SAMPLE = `${SONG_TITLE_SAMPLE} by ${SONG_ARTIST_SAMPLE}`;

/** Stand-ins for the chat room (scope.Chatters): how many people the bot has
 * watched speak recently, and one of their names. Neither is gated by a
 * module, but neither is knowable from a response being typed in a dashboard
 * either, so the preview shows a plausible room rather than a live one.
 *
 * Chat draws a DIFFERENT name for each {random.chatter} span (they are
 * independent draws); the preview shows the same one every time, for the
 * reason it does not re-roll {random} on every keystroke: a preview that
 * changed under the cursor would be read as the bot being indecisive, and a
 * second invented name would suggest the dashboard knows who is in the room.
 * The count is a plausible small room rather than a round number, so nobody
 * reads it as a placeholder the bot failed to fill. */
export const CHATTERS_SAMPLE = '37';
export const RANDOM_CHATTER_SAMPLE = 'maya_live';

/** {random.viewer}: one name from who Twitch reports as connected right now
 * (scope.Chatters' Viewers half), not from who has spoken — a different
 * source from RANDOM_CHATTER_SAMPLE, so a template naming both in one
 * preview does not appear to draw the same list twice. A dashboard preview
 * cannot read the live chat list any more than it can read the roster, so
 * this is a plausible lurker rather than a live one. */
export const RANDOM_VIEWER_SAMPLE = 'quiet_lurker';

/** Stand-ins for the emote catalog (scope.Emotes): the global 7TV, BTTV and
 * FFZ code lists the bot already keeps loaded, and one code drawn from them.
 *
 * A few plausible codes rather than the real sets, which run to hundreds of
 * codes and are truncated to one chat line before they are sent: a preview
 * that filled the editor with 480 bytes of emote names would bury the
 * response being written, and the dashboard has no catalog of its own to be
 * honest with anyway. The guide is where the real length is described.
 *
 * Repeated {random.emote} spans draw independently in chat; the preview shows
 * the same code every time, for the reason it does not re-roll {random} on
 * every keystroke. */
export const SEVENTV_EMOTES_SAMPLE = 'PagMan Clap peepoHappy';
export const BTTV_EMOTES_SAMPLE = 'KEKW monkaS catJAM';
export const FFZ_EMOTES_SAMPLE = 'LUL ZULUL AYAYA';
export const RANDOM_EMOTE_SAMPLE = 'KEKW';

/** Stand-ins for the channel itself (scope.Channel): the live title, the
 * category, how long the stream has been up and how many people are watching.
 * All four come from one Twitch read the dashboard cannot make while a
 * response is being typed, so the preview shows a plausible LIVE channel.
 *
 * Live is the choice worth naming: an offline channel previews as an uptime
 * of nothing and a viewer count of 0, which would show a broadcaster four
 * variables and two blanks, and blanks read as a bot that does not work. The
 * chip hints and the guide carry the offline behaviour instead.
 *
 * The uptime is worded by the bot's shared humanizer, the same one !uptime
 * prints through. A named channel ({game:pokimane}) previews with the same
 * stand-in as the bare spelling, for the reason the viewer lookups do: what
 * somebody else is playing is exactly what a preview cannot know. */
export const UPTIME_SAMPLE = '2 hours, 15 minutes';
export const TITLE_SAMPLE = 'bagel baking and chill';
export const GAME_SAMPLE = 'Just Chatting';
export const CHANNEL_VIEWERS_SAMPLE = '128';

/** {followers}/{subs}: the two headline audience counts (scope.Channel's
 * ChannelCounts half), read under two different Twitch identities so either
 * can degrade on its own. Plausible round-ish numbers rather than the same
 * stand-in twice, so a template that shows both reads as two different
 * counters rather than one value pasted in two places. */
export const FOLLOWERS_SAMPLE = '2,480';
export const SUBS_SAMPLE = '96';
