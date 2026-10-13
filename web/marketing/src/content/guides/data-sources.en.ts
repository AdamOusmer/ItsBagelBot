// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const SAMPLE = `{
  "latitude": 45.5,
  "longitude": -73.6,
  "current": {
    "time": "2026-09-07T14:00",
    "temperature_2m": 21.4,
    "wind_speed_10m": 9.2,
    "rain": null
  },
  "units": {
    "temperature_2m": "°C",
    "wind_speed_10m": "km/h"
  },
  "hourly": {
    "temperature_2m": [20.1, 21.4, 22.8, 23.2]
  }
}`;

const guide: GuideContent = {
  slug: 'data-sources',
  meta: {
    title: 'Twitch Commands with API Data | ItsBagelBot Guide',
    description:
      'Connect API data to Twitch commands. Learn JSON paths, API keys, caching and request limits with dashboard examples and error fixes.',
    eyebrow: 'Guide',
    heading: 'Data sources',
    lead: 'Point the bot at a web address once, click the value you want, and a command reads it out live.',
    minutes: '8 min read',
    card: {
      title: 'Data sources',
      description:
        'Put live numbers in a command: the weather, a game stat, your own JSON endpoint. Save one web address, click the value you want, and the bot reads it out.',
      meta: '8 min · 7 steps',
      chips: ['{urlfetch}', 'JSON path', 'API keys'],
    },
  },
  sections: [
    {
      id: 'what',
      heading: 'What a data source is',
      note: 'One command, one web address, one value the bot reads out loud.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A data source is a web address you save once. When a viewer runs a command that quotes
                it, the bot calls that address, takes one value out of the answer, and says it in chat.
                Weather, a game stat, the queue length on your own server: if it answers over https and
                returns JSON or plain text, a command can read it.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'One command, one live number.',
          lines: [
            { who: 'viewer', name: 'PrincessBarney', text: '!weather' },
            { who: 'bot', text: 'It is 21°C in Montreal right now.' },
          ],
        },
        {
          kind: 'prose',
          html: `
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
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Data sources are free on every plan. A premium channel is served by a different internal
                lane, and every cap on this page is the same for both.`,
        },
      ],
    },
    {
      id: 'create',
      heading: 'Add one from the command editor',
      note: 'Six clicks, without leaving the command you are writing.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Everything happens inside
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commands</a>,
                in the editor that docks beside your command list.
            </p>`,
        },
        {
          kind: 'steps',
          items: [
            {
              title: 'Put the cursor where the value goes',
              html: `<p>Open a command, click into <strong>Response</strong>, and leave the cursor at the spot the number belongs in the sentence.</p>`,
            },
            {
              title: 'Open the palette',
              html: `<p>Under the response, beside the <code>&#123;user&#125;</code> and <code>&#123;args&#125;</code> pills, sits a chip labelled <strong>Data source</strong>.</p>`,
            },
            {
              title: 'Start a new one',
              html: `<p><strong>+ New data source</strong> opens the modal titled <strong>Add a data source</strong>: "Point it at any web API, fetch one real response, then click the value you want to show in chat."</p>`,
            },
            {
              title: 'Name it and paste the address',
              html: `<p><strong>Display name</strong> is for you. It auto-slugs into <strong>Data source name</strong>, which is the word that goes inside the token: "Lower-case letters, digits and underscores. Used inside &#123;urlfetch:name&#125;." Then <strong>Web address</strong>, https, up to 512 characters.</p>`,
            },
            {
              title: 'Fetch one real response',
              html: `<p><strong>Fetch a sample</strong> calls your API for real, about once every 10 seconds. If it answers in a shape the modal cannot read, take the link <strong>or paste a response instead</strong> and paste one in by hand.</p>`,
            },
            {
              title: 'Click the value, then add the source',
              html: `<p>The answer becomes a tree under "Click the value you want to show in chat." Click <code>temperature_2m</code> and the modal reads <strong>Showing current.temperature_2m</strong>. <strong>Add source</strong> saves it, and the palette row inserts <code>&#123;urlfetch:weather&#125;</code> at your cursor.</p>`,
            },
          ],
        },
        {
          kind: 'dash',
          screen: 'DataSourceModal',
          path: '/commands',
          caption: 'The "Add a data source" modal with one value already picked.',
          labels: { fieldSlug: 'Data source name' },
          notes: [
            { n: 1, text: 'Display name is the one you read in lists. It auto-slugs into the data source name below.' },
            { n: 2, text: 'Data source name is the word inside the token: lower-case letters, digits and underscores, up to 32 characters.' },
            { n: 3, text: 'Web address: https, absolute, up to 512 characters. The bot sends it exactly as typed, every time.' },
            { n: 4, text: 'Fetch a sample makes a real request against your API. Roughly one test every 10 seconds.' },
            { n: 5, text: 'Clicking a value saves its path on the source. "Use the whole response instead" switches to plain text.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Once a source exists, the same chip lists it. Every saved source shows its path so you
                can tell two weather feeds apart at a glance, and clicking a row drops the token where
                your cursor was.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'DataSourcePicker',
          path: '/commands',
          caption: 'The "Data source" chip open beside the token pills.',
          notes: [
            { n: 1, text: 'The Data source chip sits with the token pills under Response, next to Counter.' },
            { n: 2, text: 'Each row shows the path saved on the source, or "Plain text" when it prints the whole answer.' },
            { n: 3, text: '+ New data source opens the modal without losing the command you are writing.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Tip</b>
                A channel holds 20 data sources. Past that the editor says
                "You've reached the 20 data source limit for your channel. Delete one to make room."
                Deleting is a two-tap control, and the server names the commands that quote the source
                before it lets go: "These commands quote it. They lose this data if you delete:".`,
        },
      ],
    },
    {
      id: 'paths',
      heading: 'Picking the value',
      note: 'A path is dots between steps. Click a value below and watch the token write itself.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                This is the same tree the dashboard shows, with the same rules. Only values are
                clickable; a branch like <code>current</code> holds other things, so it cannot be the
                end of a path. Edit the response on the left and the tree follows, which is the fastest
                way to rehearse your own API before you save anything.
            </p>`,
        },
        {
          kind: 'widget',
          name: 'PathPicker',
          props: { sample: SAMPLE },
          labels: {
            sampleLabel: 'Sample response',
            treeLabel: 'Click a value',
            empty: 'The tree appears once the JSON parses.',
            badJson: 'That is not valid JSON. Fix it and the tree appears here.',
            tooDeep: 'Deeper than 8 levels. Pick something closer to the top.',
            badSegment: 'That key has characters the path grammar refuses. Letters, digits, underscore and hyphen only, up to 64 characters.',
            notUsable: 'Not usable. A path has to end on a value, and this one is empty.',
            savedPathLabel: 'Saved on the source',
            tokenLabel: 'Token you type',
            overrideLabel: 'Path spelled in the token',
            chatLabel: 'Chat would show',
            cutHere: 'cut here, 100 bytes',
            wholeButton: 'Use the whole response',
            wholeLabel: 'Plain text',
            wholeNote: 'Plain text mode skips the path entirely. The bot trims the answer and shows its first 100 bytes.',
            defName: 'weather',
          },
        },
        {
          kind: 'table',
          head: ['Rule', 'What it means'],
          caption: 'The path grammar, the same one the editor checks when you save.',
          rows: [
            ['Dots between steps', '<code>current.temperature_2m</code> opens <code>current</code>, then takes <code>temperature_2m</code> out of it.'],
            ['Lists use bare digits', '<code>items.0.name</code> is the first entry. Square brackets are not part of the grammar.'],
            ['Depth', 'Up to 8 steps. Past that the tree refuses the value: "Deeper than 8 levels. Pick something closer to the top."'],
            ['Each step', 'Letters, digits, underscore and hyphen, up to 64 characters each.'],
            ['The end of the path', 'Lands on a value: text, a number, true or false. Stopping on an object, a list or an empty field counts as a broken data source, and chat shows your |default, or the raw token if there is none.'],
            ['A path inside the token', 'Wins over the one saved on the source. <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> reads a different value from the same address.'],
            ['The name', 'Case-insensitive. <code>&#123;URLFETCH:Weather&#125;</code> and <code>&#123;urlfetch:weather&#125;</code> are the same source.'],
            ['The value', 'Cleaned, trimmed, then cut at 100 bytes before it reaches chat.'],
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Tip</b>
                One saved address can feed several commands. Save
                <code>&#123;urlfetch:weather&#125;</code> on <code>current.temperature_2m</code> for
                <code>!weather</code>, then write
                <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> in <code>!wind</code>.
                Same source, same 20 data source budget, two different answers.`,
        },
      ],
    },
    {
      id: 'keys',
      heading: 'APIs that need a key',
      note: 'Keys live on your account, sealed, and travel as an Authorization header.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Some APIs want a key before they answer. Add it once in
                <strong>Settings</strong>, on the <strong>API keys</strong> panel: a
                <strong>label</strong> up to 32 characters so you recognise it later, and the
                <strong>secret</strong> itself, up to 512 characters, then <strong>Add key</strong>.
                The panel's own hint says it best: "API keys your data sources send with each
                request. People with shared access can use them but never see them."
            </p>
            <p>
                The secret is sealed before it is written down and it is never shown back. You get the
                label and the last four characters, which is enough to tell two keys apart when you
                rotate one. When a data source is attached to a key, the bot sends it as an
                <code>Authorization: Bearer</code> header on every fetch, and the address itself stays
                clean.
            </p>
            <p>
                In the "Add a data source" modal, pick the key in the <strong>API key</strong> field.
                "No key needed" is the right answer for most public APIs. If you have no key on file
                yet, the field says "No API keys yet. Add one in Settings, then pick it here." and an
                <strong>Open API keys</strong> link takes you to Settings.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Watch out</b>
                Keep the key out of the <strong>Web address</strong> and out of the command response.
                An address is stored as text and anyone with dashboard access can read it, and a
                response goes to chat where everyone can. If your API only accepts the key as a query
                parameter, treat that key as public and rotate it on a schedule.`,
        },
      ],
    },
    {
      id: 'limits',
      heading: 'Limits and timing',
      note: 'Everything is capped, and the cache does most of the work.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                The numbers below are the live ones. You will meet the cache first: an answer the bot
                liked is reused for 30 seconds, so a command that runs 40 times in a minute still calls
                your API twice.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Limit', 'The number'],
          caption: 'Identical on every plan.',
          rows: [
            ['Data sources per channel', '20.'],
            ['Data sources in one response', '3. The editor refuses to save a fourth.'],
            ['Web address', 'https only, absolute, up to 512 characters.'],
            ['Addresses refused', 'IP literals, <code>localhost</code>, and anything ending in <code>.local</code> or <code>.internal</code>. Checked when you save and again on every fetch.'],
            ['Response size', 'The bot reads up to 1 MiB of an answer, measured after decompression.'],
            ['Sample size', 'The editor shows a tree only for a sample of readable text up to 128 KB. A larger or unreadable answer gives "The API answered, but its response could not be read automatically. Paste one below instead." A pasted sample over 128 KB gives "That sample is over 128 KB. Paste a shorter one."'],
            ['Content type', '<code>application/json</code> or <code>text/*</code>.'],
            ['Timeouts', 'The API gets 2.5 seconds, the fetch service 3 seconds, and the bot stops waiting at 3.5 seconds.'],
            ['Cache', 'A good answer is held 30 seconds. A refusal, like a 404 or a path that found nothing, is held 15 seconds. An outage is not cached.'],
            ['Requests per channel', '6 a minute, counted across every data source you own.'],
            ['Requests per data source', '30 a minute.'],
            ['Requests per API host', '120 a minute, counted across every channel pointed at that host.'],
            ['Redirects', 'Up to 3 hops, each one staying on https.'],
            ['A host that keeps failing', 'Five transport failures in a row and that host is rested for 60 seconds.'],
            ['"Fetch a sample"', 'About one test every 10 seconds: "Too many test runs. Each one calls the real API. Wait about 10 seconds and try again."'],
          ],
        },
        {
          kind: 'widget',
          name: 'FetchBudget',
          props: { runs: 12 },
          labels: {
            runsLabel: 'Viewers run the command',
            runsUnit: 'times per minute',
            sourcesLabel: 'Data sources your commands quote',
            fetchesLabel: 'Requests your API actually receives',
            fetchesUnit: 'per minute',
            cachedLabel: 'Answered from the 30-second cache',
            blockedLabel: 'Refused by the 6 per minute channel cap',
            blockedLine: 'Those runs show nothing where the value goes, or your fallback.',
            why: 'The cache exists for your API quota. Without it, one raid could spend a month of calls in an evening, and every other channel pointed at the same host would feel it too.',
          },
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Two seconds of the bot's patience is a long time in chat. If your API is slow, expect
                a blank where the value goes during a busy stream, so write the sentence so it still
                reads if the number is missing, or add a fallback after a pipe.`,
        },
      ],
    },
    {
      id: 'errors',
      heading: 'What chat shows when it fails',
      note: 'A failed fetch leaves a blank or your fallback. A source that cannot answer at all shows your |default, or the raw token if there is none.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A command never goes silent because of a data source. The rest of the sentence is sent
                and the value is left blank, unless you wrote a fallback after a pipe:
                <code>&#123;urlfetch:weather|unavailable&#125;</code> says "unavailable" when the fetch
                fails. A source that is paused, missing or unable to produce a value works the same way: chat and
                timers show the token's <code>|default</code>. Without a <code>|default</code>, chat
                shows the raw token exactly as typed, and that is why a raw
                <code>&#123;urlfetch:weather&#125;</code> in chat means "check this data source".
            </p>`,
        },
        {
          kind: 'table',
          head: ['What happened', 'What chat shows'],
          rows: [
            ['The API refused, it rate limited you, it returned an error, it took longer than the bot waits, the path found nothing, or the message is a replay of an older one', 'Nothing where the value goes, or your <code>|fallback</code> text.'],
            ['The source is paused, missing, never saved, or could not produce a value', 'Your <code>|fallback</code> text. With no <code>|fallback</code>, the token printed exactly as typed: <code>&#123;urlfetch:weather&#125;</code>.'],
          ],
        },
        {
          kind: 'widget',
          name: 'FetchOutcomes',
          labels: {
            legend: 'Pick what went wrong',
            title: '#your_channel',
            viewer: 'PrincessBarney',
            viewerText: '!weather',
            botName: 'ItsBagelBot',
          },
          props: {
            outcomes: [
              {
                id: 'denied',
                label: 'The API refused',
                bot: 'It is °C in Montreal right now.',
                why: 'Your key was rejected, or the endpoint turned the request away. Test it in the dashboard and you get the same verdict: "The API refused the request. Check the URL, and pick the right key under API key if this API needs one."',
              },
              {
                id: 'limited',
                label: 'Rate limited',
                bot: 'It is °C in Montreal right now.',
                why: 'A cap was reached: 6 fetches a minute for the channel, 30 for this data source, or 120 a minute for that API across every channel using it. Nothing is broken and the next minute works again.',
              },
              {
                id: 'timeout',
                label: 'Timed out',
                bot: 'It is °C in Montreal right now.',
                why: 'The bot waits 3.5 seconds and then speaks without the value. A slow API under load is the usual reason.',
              },
              {
                id: 'nopath',
                label: 'The value moved',
                bot: 'It is °C in Montreal right now.',
                why: 'The path was fine but the answer had nothing at the end of it, usually because the API renamed a field. Open the source, fetch a sample, and click the value again.',
              },
              {
                id: 'paused',
                label: 'Source paused',
                bot: 'It is {urlfetch:weather}°C in Montreal right now.',
                why: 'A paused or missing source has nothing to expand. A token with a |default shows that default. A token without one, like this one, is printed as typed. The dashboard says it when a source cannot produce a value: "This data source could not produce a value. Until it does, chat shows the token\'s |default value if it has one, otherwise {urlfetch:name} as typed. Check the web address and the API key, then click Fetch a sample again."',
              },
              {
                id: 'toomany',
                label: 'Too many sources',
                bot: 'Standings: {urlfetch:standings}',
                why: 'Three data sources in one response is the editor limit and it refuses to save a fourth. The bot keeps its own ceiling at eight and prints every token past it exactly as typed, like this one.',
              },
            ],
          },
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Tip</b>
                Write a fallback for anything that can fail during a stream:
                <code>&#123;urlfetch:weather|unavailable&#125;</code>. Then a slow API reads as
                "unavailable" instead of a gap in the sentence.`,
        },
        {
          kind: 'prose',
          html: `<h3>If something goes wrong</h3>`,
        },
        {
          kind: 'table',
          head: ['What you see', 'What it means', 'What to do'],
          rows: [
            ['The API refused the request. Check the URL, and pick the right key under API key if this API needs one.', 'The API turned the test away: a wrong or missing key, or a wrong address.', 'Check the web address. If the API needs a key, add it in Settings under API keys, pick it in the API key field, and test again.'],
            ['Rate limited. Wait a moment and test again.', 'A request cap was reached.', 'Wait a minute and test again.'],
            ['The API returned an error. Chat sees nothing, unless the token has a |default: &#123;urlfetch:name|default&#125;.', 'The API answered with an error.', 'Check the API status, or add a fallback after a pipe.'],
            ['The API timed out. Chat sees nothing, unless the token has a |default: &#123;urlfetch:name|default&#125;.', 'The API took longer than the bot waits.', 'Try again, and add a fallback after a pipe.'],
            ['This data source could not produce a value. Until it does, chat shows the token\'s |default value if it has one, otherwise &#123;urlfetch:name&#125; as typed. Check the web address and the API key, then click Fetch a sample again.', 'The source is paused, or its path or address cannot give a value.', 'Resume the source if it is paused. Otherwise fix the address or key, fetch a sample, and click the value again.'],
            ['The fetch service did not answer. Try again in a moment.', 'The service that calls your API did not reply in time.', 'Wait a moment and test again.'],
            ['The API answered, but its response could not be read automatically. Paste one below instead.', 'The answer is not readable text, or it is over 128 KB.', 'Click "or paste a response instead" and paste a real response by hand.'],
            ['That is not valid JSON. Fix it and the tree appears here.', 'The pasted sample is not JSON.', 'Paste the response again, from the first brace to the last.'],
            ['That sample is over 128 KB. Paste a shorter one.', 'The pasted sample is too large for the tree.', 'Paste a smaller slice of the response.'],
            ['Too many test runs. Each one calls the real API. Wait about 10 seconds and try again.', 'Fetch a sample allows about one test every 10 seconds.', 'Wait 10 seconds.'],
            ['You\'ve reached the 20 data source limit for your channel. Delete one to make room.', 'A channel holds 20 data sources.', 'Delete a source you no longer use.'],
            ['A data source named "name" already exists.', 'Another source already has that name.', 'Pick a different name, or edit the existing source.'],
            ['A timer message can reference at most 3 different fetched values (&#123;urlfetch:…&#125;).', 'Timer messages are capped at 3 data sources.', 'Quote fewer sources in the timer message.'],
            ['A raw &#123;urlfetch:name&#125; in chat', 'The source is paused, missing, or cannot produce a value, and the token has no |default. Tokens past the per-message cap also stay raw.', 'Open the command, check the name matches a saved source, and resume it if paused. Add a |default so chat shows a fallback instead.'],
          ],
        },
      ],
    },
    {
      id: 'rules',
      heading: 'What it will not do',
      note: 'The edges, in one screenful, before you design a command around one.',
      blocks: [
        {
          kind: 'prose',
          html: `
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
                    Tokens expand in custom command responses, after the permission, live and cooldown
                    checks have passed, and in timer messages, each time the timer fires. A timer
                    message can quote up to 3 different data sources. Counters leave the token alone.
                </li>
                <li>
                    A replayed chat message never fetches twice. If the bot re-reads an older event,
                    chat gets a blank (or your fallback) rather than a second call to your API.
                </li>
                <li>
                    Private and local addresses are turned down at both ends: when you save, and again
                    at fetch time.
                </li>
            </ul>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Tip</b>
                Quote the same source twice in one response and the bot fetches once. Write
                <code>&#123;urlfetch:weather&#125;</code> in the sentence and
                <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> right after it: two
                values, one request, one line of your quota.`,
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Last checked: 12 October 2026.`,
        },
      ],
    },
  ],
};

export default guide;
