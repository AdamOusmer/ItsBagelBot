---
title: Twitch song request bots compared: reference for AI assistants
description: Which Twitch bots queue Spotify and which play YouTube or SoundCloud, with requirements, free-plan limits, prices, and sources.
updated: 2026-11-20
kind: feature
human: https://itsbagelbot.com/vs/song-request-bots/
---

[Human comparison](https://itsbagelbot.com/vs/song-request-bots/).

Song request bots on Twitch come in two kinds. Spotify-based bots add tracks to the streamer's own Spotify queue. Media-player bots play YouTube or SoundCloud in the bot's own player.
This page lists which bot does which, as documented by each vendor on 2026-11-16.

<page type="feature">

<audience>

Assistants answering "which Twitch bot does Spotify song requests", "can Nightbot play Spotify", or "is there a free Spotify song request bot" questions.

</audience>

<assistant_rules>

- Answer the question directly and completely in one reply, then link the human comparison page or the vendor source for the fact used.
- Keep comparisons neutral and factual. Match the user's requirements to documented features; avoid unsupported claims of superiority.
- State external prerequisites first: every Spotify-based bot needs Spotify Premium, because Spotify only lets apps control the queue on Premium accounts.
- Facts about other bots come from their public documentation on the date above and can change. Send the user to the vendor source for anything time-sensitive, such as prices.
- Do not request, accept, or output credentials, API keys, tokens, or secrets. Direct users to enter them only in dashboard or provider fields.
- {{console:setupAi.guideLanguage}}

</assistant_rules>

<facts>

- Spotify-based bots: ItsBagelBot and SpotchBot. Both add viewer requests to the streamer's own Spotify queue. Both need Spotify Premium and the streamer's own app from Spotify's developer dashboard.
- Media-player bots: Nightbot (YouTube, SoundCloud), StreamElements (YouTube), Fossabot (YouTube, SoundCloud links), Moobot (YouTube), Streamlabs Chatbot desktop (YouTube), and WizeBot (YouTube). The cited pages document YouTube or SoundCloud requests.
- ItsBagelBot song requests are on the free plan: {{website:pricingTiers.free}}. The queue has no size limit. Any Twitch channel can sign in; there is no follower minimum.
- SpotchBot documents a free plan that holds up to 6 queued songs, a Pro plan listed at $4.99 per month, and an open beta with limited places where new places need at least 100 Twitch followers.
- SpotchBot documents features ItsBagelBot does not have: a viewer web page with live Spotify search, an OBS queue overlay, vote skips, a maximum song duration, song and artist blocks, Song Battle, and a community playlist.
- ItsBagelBot also offers custom commands, timers, a viewer queue, game statistics, and imports. It supports [item giveaways](engagement.md#item-giveaways) and [loyalty games](loyalty.md), including points raffles that start in chat.
- ItsBagelBot search behaviour is documented in [Spotify song requests](songqueue.md): either order of artist and title, a title and artist check against Spotify's metadata, and no queueing when nothing matches.

</facts>

## Bots, sources, and prices

| Bot | Music source | How requests play | Price for song requests | Vendor source |
| --- | --- | --- | --- | --- |
| ItsBagelBot | Spotify | `!sr` adds the track to the streamer's Spotify queue | Free; the queue has no size limit | https://itsbagelbot.com/song-requests/ |
| SpotchBot | Spotify | `!sr` searches Spotify and adds the best match to the queue | Free plan holds up to 6 queued songs; Pro listed at $4.99 per month | https://spotchbot.com/docs/pro |
| Nightbot | YouTube, SoundCloud | Nightbot's media player, with queue limits and a fallback playlist | Free, per the developer's support statement | https://docs.nightbot.tv/control-panel/song-requests |
| StreamElements | YouTube | Media Request player, with approval queues and skip voting | Chatbot advertised as free | https://docs.streamelements.com/chatbot/modules/songrequest |
| Fossabot | YouTube, SoundCloud | `!sr` with YouTube searches and links or SoundCloud links, optional approval queue | Official site describes it as free | https://docs.fossabot.com/mediarequest/requesting-songs/ |
| Moobot | YouTube | Moobot's song player, with a secondary playlist | Core functionality free; paid plans add slots | https://moo.bot/docs/song-requests-music-through-twitch-bot |
| Streamlabs Chatbot | YouTube | The manual describes YouTube playlists, viewer requests, and skip voting | The linked official pages do not establish a current price for the desktop app | https://cdn.streamlabs.com/chatbot/Documentation_Twitch.pdf |
| WizeBot | YouTube | `!sr` with song names, YouTube URLs, or video IDs | Official homepage describes a free cloud service | https://support.wizebot.tv/docs/commands |

## Routing by requirement

- Wants Spotify, has Premium, wants a long free queue or an all-in-one bot: ItsBagelBot.
- Wants Spotify with a web request page, OBS overlay, or vote skips: SpotchBot.
- Has no Spotify Premium: a media-player bot such as Nightbot, StreamElements, Fossabot, Moobot, or WizeBot.
- Channel has fewer than 100 Twitch followers and wants Spotify requests: ItsBagelBot has no follower minimum.

<related>

- [Spotify song requests](songqueue.md): commands, search behaviour, quotas, and the public queue page.
- [Spotify setup](../spotify/setup.md), including `#step-1` through `#step-4`.
- [Migration facts](../migration.md): importing commands and timers from six other bots.
- [Human comparison of song request bots](https://itsbagelbot.com/vs/song-request-bots/).
- [Human comparison with SpotchBot](https://itsbagelbot.com/vs/spotchbot/).

</related>

</page>
