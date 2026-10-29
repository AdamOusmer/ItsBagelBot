// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'govee-lights',
  meta: {
    aiDoc: 'https://ai.itsbagelbot.com/govee/setup.md',
    title: 'Set Up Govee Lights for Twitch Rewards | ItsBagelBot',
    description:
      'Connect Wi-Fi colour Govee lights, add channel-point rewards and test colours. Follow API key setup and troubleshoot device access.',
    eyebrow: 'Guide',
    heading: 'Govee Lights',
    lead: 'Let viewers change the colour of your Govee lights with channel points. This guide covers the key, the setup wizard, the reward, and the messages you might see.',
    minutes: '6 min read',
    card: {
      title: 'Govee Lights',
      description:
        'Get your Govee API key, connect it in the dashboard, create the channel-points reward, and fix the error messages.',
      meta: '6 min · 4 steps',
      chips: ['Govee API key', 'channel points', 'colours', 'troubleshooting'],
    },
  },
  sections: [
    {
      id: 'need',
      heading: 'What you need',
      note: 'A Govee Home account, Wi-Fi colour lights set up in the Govee Home app, and a Twitch channel that can have channel-points rewards.',
      blocks: [
        {
          kind: 'callout',
          tone: 'note',
          html: '<b>Using an AI assistant?</b> Give it <a href="https://ai.itsbagelbot.com/govee/setup.md">ai.itsbagelbot.com/govee/setup.md</a>. That page is written for assistants and uses the exact button names.',
        },
        {
          kind: 'prose',
          html: `
            <p>
                You need a Govee Home account and Govee lights that can change colour, connect to
                Wi-Fi, and are already set up in the Govee Home app. Bluetooth-only lights do not
                work, because Govee's API cannot reach them. You also need a Twitch channel
                that can create channel-points rewards. The dashboard's Channel points page says
                that Twitch only lets Affiliate and Partner channels create them. Viewers spend
                channel points to change the colour of your lights.
            </p>
            <p>
                The dashboard has an "Ask your AI for help" button on each setup step. It copies a
                short prompt with the step you are on, so an AI assistant can read this guide and
                help you. Never paste your Govee API key into an AI.
            </p>`,
        },
      ],
    },
    {
      id: 'key',
      heading: 'Get your Govee API key',
      note: 'The key comes from the Govee Home app and usually arrives by email.',
      blocks: [
        {
          kind: 'steps',
          items: [
            {
              title: 'Open the Govee Home app',
              html: '<p>Open the Govee Home app on your phone and sign in to the account that owns your lights.</p>',
            },
            {
              title: 'Go to the key form',
              html: '<p>Go to <strong>Profile → Settings → "Apply for API Key"</strong>. On some app versions it is under <strong>About us</strong>.</p>',
            },
            {
              title: 'Fill in the short form',
              html: '<p>Fill in the short form and submit it.</p>',
            },
            {
              title: 'Wait for the email',
              html: '<p>Govee usually sends the key by email to your Govee account address. If it does not arrive, check your spam folder, then apply again.</p>',
            },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>A new key turns off your older keys</b>
                Generating a new key on your Govee account disables every other active key on that
                account. If another tool uses an older key, it stops working. Apply for a new key
                only when you need one.`,
        },
      ],
    },
    {
      id: 'wizard',
      heading: 'Connect the key in the dashboard',
      note: 'Open the Govee Lights page. The first time, a four-step wizard walks you through it.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Open the <a href="https://dashboard.itsbagelbot.com/govee" target="_blank" rel="noopener noreferrer">Govee Lights</a>
                page in the dashboard. The wizard has four steps: "What you need", "Get your key",
                "Paste your key" and "Your lights".
            </p>`,
        },
        {
          kind: 'steps',
          items: [
            {
              title: 'Start the wizard',
              html: '<p>On "What you need", select <strong>"Let’s set up Govee"</strong>.</p>',
            },
            {
              title: 'Check the path',
              html: '<p>On "Get your key", the dashboard shows the same path as above under "In the Govee Home app". It also says: "A new key turns off any older key on your Govee account." Select <strong>"I have my key"</strong> when the email has arrived.</p>',
            },
            {
              title: 'Paste your key',
              html: '<p>On "Paste your key", paste the key from the Govee email into the <strong>"Govee API key"</strong> field. The key is stored encrypted and never shown back.</p>',
            },
            {
              title: 'Save it',
              html: '<p>Select <strong>"Save &amp; continue"</strong>. The dashboard checks the key with Govee first.</p>',
            },
            {
              title: 'See your lights',
              html: '<p>On "Your lights", the dashboard shows "Colour lights found" and the number of lights. Select <strong>"Set up rewards"</strong> to continue.</p>',
            },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Two messages can appear when you save. They mean different things:
            </p>
            <ul>
                <li>
                    "Govee did not accept this key. Check it and try again." The key was not saved.
                    Paste it again.
                </li>
                <li>
                    "Key saved, but Govee did not answer, so it is not checked yet. Your lights will
                    work once Govee is back. If they do not, paste the key again." The key is saved
                    and you can continue.
                </li>
            </ul>`,
        },
      ],
    },
    {
      id: 'reward',
      heading: 'Create the channel-points reward',
      note: 'One reward per light. Viewers type a colour when they redeem it.',
      blocks: [
        {
          kind: 'steps',
          items: [
            {
              title: 'Open the Govee Lights page',
              html: '<p>Turn on <strong>"Enable Govee lights"</strong> if it is off. Under "Your lights", each light shows "Not set up" until it has a reward.</p>',
            },
            {
              title: 'Select a light',
              html: '<p>Select the light you want viewers to control. The reward editor opens.</p>',
            },
            {
              title: 'Fill in the reward',
              html: '<p>Set the <strong>"Reward title"</strong>, the <strong>"Cost (points)"</strong>, the <strong>"Tile colour"</strong> and the <strong>"Cooldown"</strong> (0 = off). The "Chat reply" field is optional.</p>',
            },
            {
              title: 'Choose what happens after it runs',
              html: '<p>Under "After it runs", choose "Mark fulfilled", "Refund the points" or "Leave for a mod".</p>',
            },
            {
              title: 'Create it',
              html: '<p>Select <strong>"Create reward"</strong>. The bot creates the reward on Twitch. To change it later, edit the fields and select "Save changes".</p>',
            },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>What viewers type</h3>
            <p>
                When a viewer redeems the reward, they type a colour. A colour name in English works,
                like <code>blue</code>. A hex code works too, like <code>#00ccff</code>. If you turn on
                "Let viewers turn the light off", a viewer can type <code>off</code> to turn the light off.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Live only is on by default</b>
                "Live only" means redemptions only work while your stream is live. This is
                recommended. If you turn it off, viewers can change the light even when you are
                offline.`,
        },
      ],
    },
    {
      id: 'trouble',
      heading: 'If something goes wrong',
      note: 'The messages the dashboard and the chat show for Govee lights, with what to do.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                In the chat lines below, <code>@viewer</code> stands for the name of the viewer
                who redeemed the reward. Each refund line means the bot gave the points back.
            </p>`,
        },
        {
          kind: 'table',
          id: 'govee-errors',
          head: ['What you see', 'What it means', 'What to do'],
          rows: [
            [
              '"Govee did not accept this key. Check it and try again."',
              'Govee rejected the key. The key was not saved.',
              'Copy the key again from the Govee email without extra spaces. If you applied for a newer key since, use the newest one. Older keys stop working when you generate a new one.',
            ],
            [
              '"Key saved, but Govee did not answer, so it is not checked yet. Your lights will work once Govee is back. If they do not, paste the key again."',
              'The key is saved, but Govee did not answer when the dashboard checked it.',
              'Continue. Try the reward in a while. If the lights do not change, paste the key again.',
            ],
            [
              '"Could not save the key."',
              'The dashboard could not store the key.',
              'Try again in a moment.',
            ],
            [
              '"Govee no longer accepts this API key. Choose Remove key, then paste a new key from the Govee Home app."',
              'The key was saved earlier, but Govee now rejects it. One cause is a newer key on the same account, which turns older keys off.',
              'Select "Remove key", apply for a new key in the Govee Home app, and paste it.',
            ],
            [
              '"Could not load your Govee lights. Check your API key and try again."',
              'The dashboard could not get your light list from Govee. The key may be wrong, or Govee did not answer.',
              'Select "Try again". If it keeps failing, select "Remove key" and paste the key again.',
            ],
            [
              '"No Govee devices found on this account."',
              'Govee returned no devices for this key. The lights may not be added to this Govee account, or they may only use Bluetooth.',
              'Add your lights in the Govee Home app and connect them to Wi-Fi, then select "Refresh lights".',
            ],
            [
              '"No colour-capable Govee lights found on this account."',
              'Govee returned devices, but none can change colour. Only models that support colour appear.',
              'Add a colour light to your Govee account in the Govee app, then select "Refresh lights".',
            ],
            [
              '"Could not reach the backend. Try again in a moment."',
              'The dashboard could not reach ItsBagelBot.',
              'Reload the page in a moment.',
            ],
            [
              '"Reconnect to grant channel-points access."',
              'The bot needs channel-points access to create rewards on your Twitch channel.',
              'Select "Reconnect" and approve the access on Twitch.',
            ],
            [
              "Enter your Govee API key.",
              "The key field was empty when you saved.",
              "Paste the key from the Govee email, then select \"Save &amp; continue\" again.",
            ],
            [
              "Enter a reward title.",
              "The <strong>Reward title</strong> field is empty.",
              "Type a title, then save the reward again.",
            ],
            [
              "Enter a whole number of points, at least 1.",
              "The <strong>Cost (points)</strong> is not a whole number of at least 1.",
              "Enter a whole number, 1 or more.",
            ],
            [
              "Enter a cooldown between 0 seconds and 7 days.",
              "The <strong>Cooldown</strong> is outside the allowed range.",
              "Enter a cooldown from 0 seconds (off) up to 7 days.",
            ],
            [
              "Could not toggle Govee lights.",
              "The <strong>Enable Govee lights</strong> switch did not save.",
              "Reload the page, check the switch, and try again.",
            ],
            [
              "Could not remove the key.",
              "The saved key could not be removed.",
              "Wait a moment and select \"Remove key\" again.",
            ],
            [
              '"Could not save the reward."',
              'The reward was not created or updated. One possible cause is a channel that is not a Twitch Affiliate or Partner, since the dashboard says Twitch only lets those channels create rewards.',
              'Check the title and cost, then try again. If it keeps failing, check that your channel is a Twitch Affiliate or Partner.',
            ],
            [
              "Could not delete the reward.",
              "The reward could not be deleted.",
              "Wait a moment and try again.",
            ],
            [
              '"@viewer the lights only change while the stream is live, your points were refunded."',
              'A viewer redeemed the reward while you were offline and "Live only" is on.',
              'Nothing. Go live, or turn off "Live only" if you want offline changes.',
            ],
            [
              '"@viewer that colour wasn\'t recognized, your points were refunded. Try a name like blue or a hex code like #00ccff."',
              'The viewer typed something that is not a known colour name or hex code.',
              'Tell the viewer to type an English colour name like blue, or a hex code like #00ccff.',
            ],
            [
              '"@viewer the lights are not set up yet, your points were refunded."',
              'The bot found no working Govee setup for this light when the reward was redeemed.',
              'Open the Govee Lights page. Check that "Key on file" shows and that the light has a reward.',
            ],
            [
              '"@viewer the lights are changing too often, your points were refunded. Try again in a moment."',
              'Too many colour changes came in at once, so the bot held this one back.',
              'Ask the viewer to try again in a moment. Add a "Cooldown" to the reward to space changes out.',
            ],
            [
              '"@viewer couldn\'t reach the lights, your points were refunded."',
              'Govee or the light did not answer.',
              'Check that the light is on and shows as online in the Govee Home app. Then try again.',
            ],
          ],
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                Last checked: 12 October 2026.`,
        },
      ],
    },
  ],
};

export default guide;
