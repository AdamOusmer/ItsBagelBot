# Spotify song-request search comparison

Reviewed on 2026-09-27 for the request `!sr all I wanted paramore`, which queued
`ALL I WANTED WAS YOU` by ily and EVO instead of Paramore's `All I Wanted`.

## Findings from public implementations

| Project | Inspected behavior | Relevance |
| --- | --- | --- |
| [Songify](https://github.com/songify-rocks/Songify/blob/master/Songify%20Slim/Util/Spotify/SpotifyApiHandler.cs) | Fetches multiple tracks and scores whole-query, delimiter, first-word artist and last-word artist interpretations. Low-confidence searches can still fall back to the first result. | Trailing artist interpretation addresses this request; the first-result fallback remains a weakness. |
| [Music Assistant](https://github.com/music-assistant/server/blob/dev/music_assistant/helpers/compare.py) | Checks title and artist evidence and detects recording-version conflicts. Its [track controller](https://github.com/music-assistant/server/blob/dev/music_assistant/controllers/music/media/tracks.py) searches credited artists and deduplicates candidates. | Verify both metadata fields; live, acoustic, demo and remix recordings are meaningful alternatives. |
| [spotDL](https://github.com/spotDL/spotify-downloader/blob/master/spotdl/types/song.py) | Spotify text lookup selects the first result. Its separate [cross-service matcher](https://github.com/spotDL/spotify-downloader/blob/master/spotdl/utils/matching.py) evaluates artist/title similarity and penalizes unwanted recording variants. | The stronger matcher works from known track metadata; it is not the behavior of spotDL's initial Spotify text lookup. |
| [Streamer.bot TinySpot](https://github.com/Streamer-bot-Extensions/Streamer.bot-Extensions-Wiki/blob/main/assets/spotify/files/TinySpot.sb) | Public import's embedded C# uses `limit=1` and `items[0]`. Strict requests require an exact token sequence in artist-first order. | Ordinary requests have the same failure mechanism; strict ordering would reject a title-first request. |
| [Mopidy-Spotify](https://github.com/mopidy/mopidy-spotify/blob/main/src/mopidy_spotify/translator.py) | Builds fielded artist/title queries and quotes exact phrases; presents results for selection. | Structured retrieval is useful, but this is not an automatic song-request ranker. |
| [spotify-cli](https://github.com/jantijn/spotify-cli/blob/main/internal/match/match.go) | Uses structured queries, title/artist token similarity, a confidence threshold and stable ordering for ties. | Useful deterministic metadata ranking, independent of popularity. |
| [Spotifier](https://github.com/PlayNetwork/spotifier/blob/master/lib/index.js) | Uses artist/title filters, fetches ten candidates and compares normalized artist/title edit distance. | Fetching candidates and evaluating metadata improves on selecting the first hit. |
| [Spicetify Twitch Requests](https://github.com/MrPandir/spicetify-twitch-song-requests/blob/main/src/api/spotify/search.ts) | Fetches ten results through Spotify's private desktop API, then returns the first. | Increasing the result limit without ranking does not fix incorrect selection. |

Additional established streaming tools were checked, with different evidence limits:

- [SAMMI's Spotify documentation](https://docs.christinak.ca/extensions/spotify#song-request)
  supports title followed by artist, including `Kiss from a rose Seal`; it does not
  expose the ranking algorithm.
- [MM SPOTiFY v2000 for Streamer.bot](https://mustachedmaniac.com/extensions/spotify-v2000)
  documents requests and Spotify links, but its current matching implementation was
  not available for inspection.
- [Mix It Up](https://mixitup.bot/docs/music-player) currently excludes Spotify from
  its music player. Its local-file title matching is a different problem.
- [StreamerSonglist](https://streamersonglist.com/) searches a streamer's own song
  library, rather than documenting Spotify catalog matching.

## Applied change

The provider now retrieves up to ten candidates even when `!sr` requests one
output track. It ranks normalized title and artist evidence, checks all artist
credits, and retains Spotify order for ties. Complete title-plus-artist evidence
outranks a title that merely contains the supplied artist name.

When broad results leave words unexplained, their titles support possible
artist-first or artist-last splits. At most three fielded recovery searches run;
returned metadata must account for both supplied fields. For the reported case,
the recovery query is `track:"all i wanted" artist:"paramore"`. A supported artist
interpretation that cannot be verified produces no match instead of queuing the
title-only near match. Explicit artist/title requests receive the same validation,
while a literal title such as `Stand by Me` can recover from a false convention
split. Existing deadlines bound the entire lookup, and upstream failures stop
recovery. Ordinary text results must match a title or a complete title/artist interpretation.
Artist and title identity are checked separately; one-character typos are allowed
for longer names, and exact titles outrank typo matches. Only delimited release
and featured-credit metadata is stripped; unrequested live, acoustic, remix and
other recording markers are refused. Every upstream text search is metered, with
a total ceiling of four requests inside the existing lookup deadline.

A literal full title containing an artist name remains inherently ambiguous when
none of the bounded filtered searches finds the intended recording. In that case
the literal title is retained. Artist boundaries outside the bounded candidate
window can also remain ambiguous. This is not a guarantee of perfect resolution
for every phrase; a direct track link is deterministic.

The [current Spotify search reference](https://developer.spotify.com/documentation/web-api/reference/search)
documents a maximum of ten results and `track:` / `artist:` filters. The patch does
not depend on popularity metadata.

## Link handling and verification

Direct track/album links, regional paths, tracking parameters, embed URLs and
Spotify URIs resolve by catalog ID. Equivalent direct spellings share a cache
entry. Malformed Spotify references are refused before token minting; missing
catalog items return a public error instead of becoming text searches.

Short shares (`spotify.link`, `spoti.fi`, and `open.spotify.com/s/`) are sent to the
fixed `https://open.spotify.com/oembed` endpoint. The documented iframe HTML reveals
the catalog ID. The bot does not fetch the supplied share URL or returned iframe,
and does not send broadcaster credentials to oEmbed. Unsupported or invalid
returned targets are refused. This uses Spotify's own service instead of a
third-party resolver. See the official [oEmbed tutorial](https://developer.spotify.com/documentation/embeds/tutorials/using-the-oembed-api)
and [response reference](https://developer.spotify.com/documentation/embeds/reference/oembed).

A live, unauthenticated GET to
[oEmbed for a real short share](https://open.spotify.com/oembed?url=https%3A%2F%2Fspotify.link%2FhRkBrwub9xb)
returned HTTP 200 without a redirect and revealed track ID
`7xtG2Kac0KPcd1ThPEGbZw`. A real `open.spotify.com/s/` share also resolved successfully during the review.
Two published legacy `spoti.fi` shares returned oEmbed timeouts and generic
app-activation pages; the resolver reports a failure for these rather than
searching their URL. Routing and failure handling have fixture coverage.

Regression tests use local HTTP fixtures for the reported wrong result, hidden
correct results, artist order, multiple-word artists, alternate credits, version
ranking, cache reuse, bounded recovery, throttling, direct/short links, malformed
targets and not-found errors. The authenticated live search and player queue were
not exercised. Release verification is tracked in the pull request and deployment record.


## Adversarial release review

One subagent reproduced and reviewed weak first-result matches, tribute artists,
extended wrong titles, colon punctuation, by/dash conventions, release annotations,
unsolicited recording versions, title-only preservation, exact-versus-typo ranking,
and malformed/protocol-relative links. Sixteen adversarial fixture cases passed
after hardening. Follow-up regression coverage includes multiple-word artists
inside a literal cover title and the bounded request ceiling.
