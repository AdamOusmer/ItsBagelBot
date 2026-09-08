// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "data-sources" guide in en. Copy only: the structure it fills lives in
// src/lib/guides/skeletons/data-sources.ts, and every key below is one k('...')
// there. Adding a language is this file translated, with no structure to get
// wrong; a key this locale omits falls back to English.
import type { GuideStrings } from '../../lib/guides/skeleton';

const strings: GuideStrings = {
    'create.b0.html': `
            <p>
                Everything happens inside
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commands</a>,
                in the editor that docks beside your command list.
            </p>`,
    'create.b1.items.0.html': '<p>Open a command, click into <strong>Response</strong>, and leave the cursor at the spot the number belongs in the sentence.</p>',
    'create.b1.items.0.title': 'Put the cursor where the value goes',
    'create.b1.items.1.html': '<p>Under the response, beside the <code>&#123;user&#125;</code> and <code>&#123;args&#125;</code> pills, sits a chip labelled <strong>Data source</strong>. Its tooltip reads "Insert a fetched value from a saved API definition".</p>',
    'create.b1.items.1.title': 'Open the palette',
    'create.b1.items.2.html': '<p><strong>+ New data source</strong> opens the modal titled <strong>Add a data source</strong>: "Point it at any web API, fetch one real response, then click the value you want to show in chat."</p>',
    'create.b1.items.2.title': 'Start a new one',
    'create.b1.items.3.html': '<p><strong>Display name</strong> is for you. It auto-slugs into <strong>Definition name</strong>, which is the word that goes inside the token: "Lower-case letters, digits and underscores. Used inside &#123;urlfetch:name&#125;." Then <strong>Web address</strong>, https, up to 512 characters.</p>',
    'create.b1.items.3.title': 'Name it and paste the address',
    'create.b1.items.4.html': '<p><strong>Fetch a sample</strong> calls your API for real, about once every 10 seconds. If it answers in a shape the modal cannot read, take the link <strong>or paste a response instead</strong> and paste one in by hand.</p>',
    'create.b1.items.4.title': 'Fetch one real response',
    'create.b1.items.5.html': '<p>The answer becomes a tree under "Click the value you want to show in chat." Click <code>temperature_2m</code> and the modal reads <strong>Showing current.temperature_2m</strong>. <strong>Add source</strong> saves it, and the palette row inserts <code>&#123;urlfetch:weather&#125;</code> at your cursor.</p>',
    'create.b1.items.5.title': 'Click the value, then add the source',
    'create.b2.caption': 'The "Add a data source" modal with one value already picked.',
    'create.b2.notes.0.text': 'Display name is the one you read in lists. It auto-slugs into the definition name below.',
    'create.b2.notes.1.text': 'Definition name is the word inside the token: lower-case letters, digits and underscores, up to 32 characters.',
    'create.b2.notes.2.text': 'Web address: https, absolute, up to 512 characters. The bot sends it exactly as typed, every time.',
    'create.b2.notes.3.text': 'Fetch a sample makes a real request against your API. Roughly one test every 10 seconds.',
    'create.b2.notes.4.text': 'Clicking a value saves its path on the source. "Use the whole response instead" switches to plain text.',
    'create.b3.html': `
            <p>
                Once a source exists, the same chip lists it. Every saved source shows its path so you
                can tell two weather feeds apart at a glance, and clicking a row drops the token where
                your cursor was.
            </p>`,
    'create.b4.caption': 'The "Data source" chip open beside the token pills.',
    'create.b4.notes.0.text': 'The Data source chip sits with the token pills under Response, next to Counter.',
    'create.b4.notes.1.text': 'Each row shows the path saved on the source, or "Plain text" when it prints the whole answer.',
    'create.b4.notes.2.text': '+ New data source opens the modal without losing the command you are writing.',
    'create.b5.html': `
                <b>Tip</b>
                A channel holds 20 definitions. Past that the editor says
                "You've reached the 20-definition limit for your channel. Delete one to make room."
                Deleting is a two-tap control, and the server names the commands that quote the source
                before it lets go: "These commands quote it. They lose this data if you delete:".`,
    'create.heading': 'Add one from the command editor',
    'create.note': 'Six clicks, without leaving the command you are writing.',
    'errors.b0.html': `
            <p>
                A command never goes silent because of a data source. The rest of the sentence is sent
                and the value is replaced with one of four strings, so you can read chat and tell what
                broke.
            </p>`,
    'errors.b1.head.0': 'What happened',
    'errors.b1.head.1': 'What chat shows',
    'errors.b1.rows.0.0': 'The API refused, it rate limited you, or the message is a replay of an older one',
    'errors.b1.rows.0.1': '<code>[source unavailable]</code>',
    'errors.b1.rows.1.0': 'The API returned an error, or the path found nothing usable',
    'errors.b1.rows.1.1': '<code>[source error]</code>',
    'errors.b1.rows.2.0': 'The API took longer than the bot waits',
    'errors.b1.rows.2.1': '<code>[source timed out]</code>',
    'errors.b1.rows.3.0': 'The source is missing, paused, or the token names one that was never saved',
    'errors.b1.rows.3.1': 'The token, printed exactly as typed: <code>&#123;urlfetch:weather&#125;</code>',
    'errors.b2.labels.botName': 'ItsBagelBot',
    'errors.b2.labels.legend': 'Pick what went wrong',
    'errors.b2.labels.title': '#your_channel',
    'errors.b2.labels.viewer': 'sesame_sam',
    'errors.b2.labels.viewerText': '!weather',
    'errors.b2.props.outcomes.0.bot': 'It is [source unavailable]°C in Montreal right now.',
    'errors.b2.props.outcomes.0.label': 'The API refused',
    'errors.b2.props.outcomes.0.why': 'Your key was rejected, or the endpoint turned the request away. Test it in the dashboard and you get the same verdict: "The API refused the request. Check the key or the URL."',
    'errors.b2.props.outcomes.1.bot': 'It is [source unavailable]°C in Montreal right now.',
    'errors.b2.props.outcomes.1.label': 'Rate limited',
    'errors.b2.props.outcomes.1.why': 'A cap was reached: 6 fetches a minute for the channel, 30 for this definition, or 120 a minute for that API across every channel using it. Nothing is broken and the next minute works again.',
    'errors.b2.props.outcomes.2.bot': 'It is [source timed out]°C in Montreal right now.',
    'errors.b2.props.outcomes.2.label': 'Timed out',
    'errors.b2.props.outcomes.2.why': 'The bot waits 3.5 seconds and then speaks without the value. A slow API under load is the usual reason.',
    'errors.b2.props.outcomes.3.bot': 'It is [source error]°C in Montreal right now.',
    'errors.b2.props.outcomes.3.label': 'The value moved',
    'errors.b2.props.outcomes.3.why': 'The path was fine but the answer had nothing at the end of it, usually because the API renamed a field. Open the source, fetch a sample, and click the value again.',
    'errors.b2.props.outcomes.4.bot': 'It is {urlfetch:weather}°C in Montreal right now.',
    'errors.b2.props.outcomes.4.label': 'Source paused',
    'errors.b2.props.outcomes.4.why': 'A paused or deleted source has nothing to expand, so the token is printed as typed. The dashboard test says it plainly: "Definition missing or paused. Chat would show the raw token until it is active."',
    'errors.b2.props.outcomes.5.bot': 'Standings: {urlfetch:standings}',
    'errors.b2.props.outcomes.5.label': 'Too many sources',
    'errors.b2.props.outcomes.5.why': 'Three data sources in one response is the editor limit and it refuses to save a fourth. The bot keeps its own ceiling at eight and prints every token past it exactly as typed, like this one.',
    'errors.b3.html': `
                <b>Note</b>
                These four strings are not translated. A French channel sees
                <code>[source unavailable]</code> too, which keeps them searchable and keeps this page
                honest about what your viewers will read.`,
    'errors.heading': 'What chat shows when it fails',
    'errors.note': 'Four fallbacks, all short, all in English wherever your chat is.',
    'keys.b0.html': `
            <p>
                Some APIs want a key before they answer. Add it once under
                <strong>Settings</strong>, on the <strong>API keys</strong> panel: a
                <strong>label</strong> up to 32 characters so you recognise it later, and the
                <strong>secret</strong> itself, up to 512 characters. The panel's own hint says it
                best: "Account-level secrets for data sources. Delegates can spend them, never read
                them."
            </p>
            <p>
                The secret is sealed before it is written down and it is never shown back. You get the
                label and the last four characters, which is enough to tell two keys apart when you
                rotate one. When a data source is attached to a key, the bot sends it as an
                <code>Authorization: Bearer</code> header on every fetch, and the address itself stays
                clean.
            </p>
            <p>
                In the "Add a data source" modal, the <strong>API key</strong> field only appears once
                you have at least one key on file. Until then it reads "No key needed", which is also
                the right answer for most public APIs.
            </p>`,
    'keys.b1.html': `
                <b>Watch out</b>
                Keep the key out of the <strong>Web address</strong> and out of the command response.
                An address is stored as text and anyone with dashboard access can read it, and a
                response goes to chat where everyone can. If your API only accepts the key as a query
                parameter, treat that key as public and rotate it on a schedule.`,
    'keys.heading': 'APIs that need a key',
    'keys.note': 'Keys live on your account, sealed, and travel as an Authorization header.',
    'limits.b0.html': `
            <p>
                The numbers below are the live ones. You will meet the cache first: an answer the bot
                liked is reused for 30 seconds, so a command that runs 40 times in a minute still calls
                your API twice.
            </p>`,
    'limits.b1.caption': 'Identical on every plan.',
    'limits.b1.head.0': 'Limit',
    'limits.b1.head.1': 'The number',
    'limits.b1.rows.0.0': 'Definitions per channel',
    'limits.b1.rows.0.1': '20.',
    'limits.b1.rows.1.0': 'Data sources in one response',
    'limits.b1.rows.1.1': '3. The editor refuses to save a fourth.',
    'limits.b1.rows.10.0': 'Requests per API host',
    'limits.b1.rows.10.1': '120 a minute, counted across every channel pointed at that host.',
    'limits.b1.rows.11.0': 'Redirects',
    'limits.b1.rows.11.1': 'Up to 3 hops, each one staying on https.',
    'limits.b1.rows.12.0': 'A host that keeps failing',
    'limits.b1.rows.12.1': 'Five transport failures in a row and that host is rested for 60 seconds.',
    'limits.b1.rows.13.0': '"Fetch a sample"',
    'limits.b1.rows.13.1': 'About one test every 10 seconds: "Too many test runs. Each one calls the real API. Wait about 10 seconds and try again."',
    'limits.b1.rows.2.0': 'Web address',
    'limits.b1.rows.2.1': 'https only, absolute, up to 512 characters.',
    'limits.b1.rows.3.0': 'Addresses refused',
    'limits.b1.rows.3.1': 'IP literals, <code>localhost</code>, and anything ending in <code>.local</code> or <code>.internal</code>. Checked when you save and again on every fetch.',
    'limits.b1.rows.4.0': 'Response size',
    'limits.b1.rows.4.1': '1 MiB, measured after decompression.',
    'limits.b1.rows.5.0': 'Content type',
    'limits.b1.rows.5.1': '<code>application/json</code> or <code>text/*</code>.',
    'limits.b1.rows.6.0': 'Timeouts',
    'limits.b1.rows.6.1': 'The API gets 2.5 seconds, the fetch service 3 seconds, and the bot stops waiting at 3.5 seconds.',
    'limits.b1.rows.7.0': 'Cache',
    'limits.b1.rows.7.1': 'A good answer is held 30 seconds. A refusal, like a 404 or a path that found nothing, is held 15 seconds. An outage is not cached.',
    'limits.b1.rows.8.0': 'Requests per channel',
    'limits.b1.rows.8.1': '6 a minute, counted across every definition you own.',
    'limits.b1.rows.9.0': 'Requests per definition',
    'limits.b1.rows.9.1': '30 a minute.',
    'limits.b2.labels.blockedLabel': 'Refused by the 6 per minute channel cap',
    'limits.b2.labels.blockedLine': 'Those runs print [source unavailable] in chat.',
    'limits.b2.labels.cachedLabel': 'Answered from the 30-second cache',
    'limits.b2.labels.fetchesLabel': 'Requests your API actually receives',
    'limits.b2.labels.fetchesUnit': 'per minute',
    'limits.b2.labels.runsLabel': 'Viewers run the command',
    'limits.b2.labels.runsUnit': 'times per minute',
    'limits.b2.labels.sourcesLabel': 'Data sources your commands quote',
    'limits.b2.labels.why': 'The cache exists for your API quota. Without it, one raid could spend a month of calls in an evening, and every other channel pointed at the same host would feel it too.',
    'limits.b3.html': `
                <b>Note</b>
                Two seconds of the bot's patience is a long time in chat. If your API is slow, expect
                <code>[source timed out]</code> during a busy stream and write the sentence so it still
                reads if the number is missing.`,
    'limits.heading': 'Limits and timing',
    'limits.note': 'Everything is capped, and the cache does most of the work.',
    'meta.card.chips.0': '{urlfetch}',
    'meta.card.chips.1': 'JSON path',
    'meta.card.chips.2': 'API keys',
    'meta.card.description': 'Put live numbers in a command: the weather, a game stat, your own JSON endpoint. Save one web address, click the value you want, and the bot reads it out.',
    'meta.card.meta': '8 min · 7 steps',
    'meta.card.title': 'Data sources',
    'meta.description': 'Read a live value from any web API into a chat command with {urlfetch}: add a data source, pick the value with a JSON path, attach an API key, and learn the caching and rate limits before chat finds them.',
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Data sources',
    'meta.lead': 'Point the bot at a web address once, click the value you want, and a command reads it out live.',
    'meta.minutes': '8 min read',
    'meta.title': 'Data sources - ItsBagelBot Guides',
    'paths.b0.html': `
            <p>
                This is the same tree the dashboard shows, with the same rules. Only values are
                clickable; a branch like <code>current</code> holds other things, so it cannot be the
                end of a path. Edit the response on the left and the tree follows, which is the fastest
                way to rehearse your own API before you save anything.
            </p>`,
    'paths.b1.labels.badJson': 'That is not valid JSON. Fix it and the tree appears here.',
    'paths.b1.labels.badSegment': 'That key has characters the path grammar refuses. Letters, digits, underscore and hyphen only, up to 64 characters.',
    'paths.b1.labels.chatLabel': 'Chat would show',
    'paths.b1.labels.cutHere': 'cut here, 100 bytes',
    'paths.b1.labels.defName': 'weather',
    'paths.b1.labels.empty': 'The tree appears once the JSON parses.',
    'paths.b1.labels.notUsable': 'Not usable. A path has to end on a value, and this one is empty.',
    'paths.b1.labels.overrideLabel': 'Path spelled in the token',
    'paths.b1.labels.sampleLabel': 'Sample response',
    'paths.b1.labels.savedPathLabel': 'Saved on the source',
    'paths.b1.labels.tokenLabel': 'Token you type',
    'paths.b1.labels.tooDeep': 'Deeper than 8 levels. Pick something closer to the top.',
    'paths.b1.labels.treeLabel': 'Click a value',
    'paths.b1.labels.wholeButton': 'Use the whole response',
    'paths.b1.labels.wholeLabel': 'Plain text',
    'paths.b1.labels.wholeNote': 'Plain text mode skips the path entirely. The bot trims the answer and shows its first 100 bytes.',
    'paths.b2.caption': 'The path grammar, the same one the editor checks when you save.',
    'paths.b2.head.0': 'Rule',
    'paths.b2.head.1': 'What it means',
    'paths.b2.rows.0.0': 'Dots between steps',
    'paths.b2.rows.0.1': '<code>current.temperature_2m</code> opens <code>current</code>, then takes <code>temperature_2m</code> out of it.',
    'paths.b2.rows.1.0': 'Lists use bare digits',
    'paths.b2.rows.1.1': '<code>items.0.name</code> is the first entry. Square brackets are not part of the grammar.',
    'paths.b2.rows.2.0': 'Depth',
    'paths.b2.rows.2.1': 'Up to 8 steps. Past that the tree refuses the value: "Deeper than 8 levels. Pick something closer to the top."',
    'paths.b2.rows.3.0': 'Each step',
    'paths.b2.rows.3.1': 'Letters, digits, underscore and hyphen, up to 64 characters each.',
    'paths.b2.rows.4.0': 'The end of the path',
    'paths.b2.rows.4.1': 'Lands on a value: text, a number, true or false. Stopping on an object, a list or an empty field counts as a broken definition, and chat gets the raw token.',
    'paths.b2.rows.5.0': 'A path inside the token',
    'paths.b2.rows.5.1': 'Wins over the one saved on the source. <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> reads a different value from the same address.',
    'paths.b2.rows.6.0': 'The name',
    'paths.b2.rows.6.1': 'Case-insensitive. <code>&#123;URLFETCH:Weather&#125;</code> and <code>&#123;urlfetch:weather&#125;</code> are the same source.',
    'paths.b2.rows.7.0': 'The value',
    'paths.b2.rows.7.1': 'Cleaned, trimmed, then cut at 100 bytes before it reaches chat.',
    'paths.b3.html': `
                <b>Tip</b>
                One saved address can feed several commands. Save
                <code>&#123;urlfetch:weather&#125;</code> on <code>current.temperature_2m</code> for
                <code>!weather</code>, then write
                <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> in <code>!wind</code>.
                Same source, same 20-definition budget, two different answers.`,
    'paths.heading': 'Picking the value',
    'paths.note': 'A path is dots between steps. Click a value below and watch the token write itself.',
    'rules.b0.html': `
            <ul>
                <li>
                    The address is fixed when you save it. <code>&#123;args&#125;</code> and
                    <code>&#123;user&#125;</code> are left alone inside a web address, so a viewer
                    cannot steer the request at your API.
                </li>
                <li>
                    Requests are <code>GET</code>, and headers are not yours to set. The one the bot
                    adds is <code>Authorization: Bearer</code>, and only when the source carries a key.
                </li>
                <li>
                    Tokens expand in custom command responses only, after the permission, live and
                    cooldown checks have passed. A timer posts its text raw, and counters leave the
                    token alone.
                </li>
                <li>
                    A replayed chat message never fetches twice. If the bot re-reads an older event,
                    chat gets <code>[source unavailable]</code> rather than a second call to your API.
                </li>
                <li>
                    Private and local addresses are turned down at both ends: when you save, and again
                    at fetch time.
                </li>
            </ul>`,
    'rules.b1.html': `
                <b>Tip</b>
                Quote the same source twice in one response and the bot fetches once. Write
                <code>&#123;urlfetch:weather&#125;</code> in the sentence and
                <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> right after it: two
                values, one request, one line of your quota.`,
    'rules.heading': 'What it will not do',
    'rules.note': 'The edges, in one screenful, before you design a command around one.',
    'what.b0.html': `
            <p>
                A data source is a web address you save once. When a viewer runs a command that quotes
                it, the bot calls that address, takes one value out of the answer, and says it in chat.
                Weather, a game stat, the queue length on your own server: if it answers over https and
                returns JSON or plain text, a command can read it.
            </p>`,
    'what.b1.caption': 'One command, one live number.',
    'what.b1.lines.0.name': 'sesame_sam',
    'what.b1.lines.0.text': '!weather',
    'what.b1.lines.1.text': 'It is 21°C in Montreal right now.',
    'what.b1.title': '#your_channel',
    'what.b2.html': `
            <p>
                Behind that line is an ordinary custom command. Its response, as typed in the editor:
            </p>
            <p><code>It is &#123;urlfetch:weather&#125;°C in Montreal right now.</code></p>
            <p>
                The token names the source, not the value. Which value the bot pulls out is saved on the
                source itself, as a <strong>path</strong>: an address inside the answer. The response is
                the building, <code>current</code> is the floor, <code>temperature_2m</code> is the
                door. Write it out with dots and you get <code>current.temperature_2m</code>. Section
                three does that clicking for you, and the analogy can go home now.
            </p>`,
    'what.b3.html': `
                <b>Note</b>
                Data sources are free on every plan. A premium channel is served by a different internal
                lane, and every cap on this page is the same for both.`,
    'what.heading': 'What a data source is',
    'what.note': 'One command, one web address, one value the bot reads out loud.',
};

export default strings;
