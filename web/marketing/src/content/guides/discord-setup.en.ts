// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: "discord-setup",
  meta: {
    title: "Discord Bot Setup and Permissions | ItsBagelBot Guide",
    description: "Add ItsBagelBot to Discord, check permissions and set up channels and roles. Fix role order and setup errors. Requires ItsBagelBot Premium during beta.",
    eyebrow: "Guide",
    heading: "Add Bagel to Discord",
    lead: "Connect a Discord server to your channel, let Bagel set it up, and know what to do when Discord says no.",
    minutes: "7 min read",
    card: {
      title: "Add Bagel to Discord",
      description: "Add the bot to your server, the permissions it asks for, the server layout, the role order fix, and the error messages.",
      meta: "7 min · 5 sections",
      chips: [
        "Premium beta",
        "permissions",
        "role order",
        "errors"
      ]
    }
  },
  sections: [
    {
      id: "need",
      heading: "What you need",
      note: "A Premium channel, and a Discord server you own or manage.",
      blocks: [
        {
          kind: "prose",
          html: "<p>You need three things. First, a Twitch account signed in to the Bagel dashboard. Second, a Premium channel: Discord is a beta module and is open to Premium channels only while the beta lasts. Third, a Discord account that owns the server, or has Manage Server on it. Bagel can only be added to a server where you have one of those.</p>"
        },
        {
          kind: "callout",
          tone: "note",
          html: "<b>Not Premium yet?</b> The Discord page shows \"Premium only while in beta\" and a \"See plans\" button. What you set up during the beta keeps working after it ends."
        }
      ]
    },
    {
      id: "add",
      heading: "Add Bagel to your server",
      note: "Seven steps, about two minutes.",
      blocks: [
        {
          kind: "steps",
          items: [
            {
              title: "Open the Discord page",
              html: "<p>In the dashboard menu, open \"Discord\".</p>"
            },
            {
              title: "Start the connection",
              html: "<p>Choose \"Add a server\". If the button is greyed out, see the table at the end of this guide.</p>"
            },
            {
              title: "Sign in to Discord if it asks",
              html: "<p>Discord opens. Use the Discord account that owns the server or has Manage Server on it. Bagel asks Discord for your server list once, to build the next page. The list is never stored.</p>"
            },
            {
              title: "Find your server",
              html: "<p>On \"Servers you manage\", look for your server. Use \"Find a server\" to search by name. Server missing? You need the Manage Server permission on it.</p>"
            },
            {
              title: "Choose \"Add Bagel here\"",
              html: "<p>Click \"Add Bagel here\" on that row. Discord opens its confirm screen.</p>"
            },
            {
              title: "Keep every permission checked and authorize",
              html: "<p>Discord lists the permissions Bagel asks for. The dashboard tells you to keep them all checked. Then choose \"Authorize\", the button the dashboard names for Discord's confirm screen.</p>"
            },
            {
              title: "Let Bagel set up the server",
              html: "<p>Bagel joins the server and sets it up. The dashboard opens that server's page. If the server already had channels, Bagel keeps them and tells you so.</p>"
            }
          ]
        },
        {
          kind: "prose",
          html: "<p>No server yet? On the Discord page, \"Create my Discord\" builds one from Bagel's template. Each server keeps its own channels, roles and settings, and you can connect as many servers as you like.</p>"
        }
      ]
    },
    {
      id: "permissions",
      heading: "Main permissions Bagel asks for",
      note: "The main ones, and why. Keep them all checked. Bagel never needs Administrator.",
      blocks: [
        {
          kind: "table",
          head: [
            "Permission",
            "Why Bagel asks"
          ],
          rows: [
            [
              "Kick Members, Ban Members, Timeout Members",
              "Moderation."
            ],
            [
              "Manage Roles, Manage Channels",
              "To set up your server: the channels and roles Bagel needs."
            ],
            [
              "Change Nickname",
              "To apply the premium name. Servers added before Bagel asked for it need \"Re-authorize Bagel\" (see the table below)."
            ],
            [
              "Plus message, voice and audit-log permissions",
              "Discord's confirm screen also lists permissions to view channels, send and manage messages, add reactions, embed links, attach files, read message history, connect to and move members in voice channels, use application commands and view the audit log. Bagel asks for 17 permissions in all. The rows above are the main ones."
            ]
          ]
        },
        {
          kind: "callout",
          tone: "warn",
          html: "<b>Keep them all checked</b> If you uncheck one on Discord's screen, Discord later refuses some actions. The fix is \"Re-authorize\" on the server's Settings page, with every permission checked. Bagel never needs Administrator."
        }
      ]
    },
    {
      id: "pick",
      heading: "Reading the server list",
      note: "What each row means and which button to use.",
      blocks: [
        {
          kind: "table",
          head: [
            "What the row says",
            "What you can do"
          ],
          rows: [
            [
              "\"Bagel is not in this server yet.\"",
              "Choose \"Add Bagel here\"."
            ],
            [
              "\"Bagel is already in this server.\"",
              "Choose \"Open\" to manage it. Choose \"Add Bagel again\" to run Discord's confirm screen once more, for example after you unchecked a permission."
            ],
            [
              "\"Bagel needs to be added again to work in this server.\"",
              "\"Add Bagel again\" is highlighted. Choose it and authorize on Discord."
            ],
            [
              "\"Linked to another channel\"",
              "Another Twitch channel already uses this server. Ask that channel's owner to open the server in Bagel, go to Settings and choose \"Disconnect\". Then add it here again."
            ]
          ]
        },
        {
          kind: "callout",
          tone: "warn",
          html: "<b>Leave other channels' servers alone</b> Do not remove Bagel from a server that is linked to someone else's channel. It is part of their setup. Only that channel's owner can free the server, by choosing \"Disconnect\"."
        }
      ]
    },
    {
      id: "layout",
      heading: "Create the channels and roles",
      note: "One button adds what is missing. Nothing is deleted.",
      blocks: [
        {
          kind: "steps",
          items: [
            {
              title: "Open your server in the dashboard",
              html: "<p>On the Discord page, choose \"Open\" next to the server.</p>"
            },
            {
              title: "Open its Settings page",
              html: "<p>In the \"Server settings\" menu, choose \"Settings\".</p>"
            },
            {
              title: "Find \"Server layout\"",
              html: "<p>The \"Server layout\" card explains what the button does.</p>"
            },
            {
              title: "Choose \"Create missing channels and roles\"",
              html: "<p>Bagel adds the channels and roles it needs that the server is missing, and uses any role you pinned on the Roles page. Nothing is deleted. A \"Server set up.\" message confirms it.</p>"
            }
          ]
        },
        {
          kind: "prose",
          html: "<p>If the server already had its own channels, Bagel keeps them and adds only what it needs. Pick the rest on the \"Channels\" page.</p>"
        }
      ]
    },
    {
      id: "roles",
      heading: "Fix the role order",
      note: "Discord only lets a bot manage roles below its own role.",
      blocks: [
        {
          kind: "prose",
          html: "<p>A bot can only manage roles that sit below its own highest role. If Bagel cannot assign a role, its role is too low in the list. This is the fix.</p>"
        },
        {
          kind: "steps",
          items: [
            {
              title: "Open Server Settings in Discord",
              html: "<p>In Discord, open \"Server Settings\" for the server.</p>"
            },
            {
              title: "Open Roles",
              html: "<p>Choose \"Roles\".</p>"
            },
            {
              title: "Drag Bagel's role higher",
              html: "<p>Drag Bagel's role above the roles it manages, then save.</p>"
            },
            {
              title: "Try again in the dashboard",
              html: "<p>Repeat the action that failed. If Discord will not let you move the role, ask the server owner to do it.</p>"
            }
          ]
        }
      ]
    },
    {
      id: "disconnect",
      heading: "Disconnect or remove Bagel",
      note: "Disconnecting and removing the bot are two different things.",
      blocks: [
        {
          kind: "steps",
          items: [
            {
              title: "Open the server's Settings page",
              html: "<p>On the Discord page, choose \"Open\" next to the server, then \"Settings\" in the \"Server settings\" menu.</p>"
            },
            {
              title: "Go to the \"Danger zone\"",
              html: "<p>Scroll to \"Danger zone\".</p>"
            },
            {
              title: "Choose \"Disconnect\"",
              html: "<p>Confirm \"Disconnect Discord?\" with \"Disconnect\". Bagel stops posting and forgets which server this channel is linked to. Your channels and roles stay exactly as they are. Bagel itself stays in the server.</p>"
            },
            {
              title: "To remove the bot, kick it in Discord",
              html: "<p>The dashboard says to right-click Bagel in the server's member list in Discord and choose \"Kick\". Kicking needs the Kick Members permission, or being the server owner. If you do not see \"Kick\", ask the server owner.</p>"
            }
          ]
        },
        {
          kind: "callout",
          tone: "tip",
          html: "<b>Re-authorize</b> The \"Danger zone\" also has \"Re-authorize\". It runs the same Discord connection again, for example after you unchecked a permission."
        }
      ]
    },
    {
      id: "trouble",
      heading: "If something goes wrong",
      note: "The messages the Discord pages show, and what to do.",
      blocks: [
        {
          kind: "table",
          head: [
            "What you see",
            "What it means",
            "What to do"
          ],
          rows: [
            [
              "\"Premium only while in beta\"",
              "Your channel is not Premium. Discord is open to Premium channels only during the beta.",
              "Choose \"See plans\". Come back to \"Discord\" once Premium is active."
            ],
            [
              "\"Discord is in beta and open to Premium channels only.\"",
              "Same limit, shown when a Discord action is refused.",
              "Make the channel Premium, then try again."
            ],
            [
              "\"Premium for this channel is managed by &#123;login&#125;. Ask them to upgrade on their Billing page.\"",
              "You are a mod with shared access, and the Billing section is not shared with you.",
              "Ask the channel owner to upgrade on their Billing page."
            ],
            [
              "The \"Add a server\" button is greyed out, or \"Discord is not configured on this deployment (missing client id or redirect).\"",
              "Discord connection is not set up on Bagel's side. Nothing in your account causes it.",
              "Contact support through the Contact page (itsbagelbot.com/contact) or at support@itsbagelbot.com."
            ],
            [
              "\"You cancelled on Discord's screen, so nothing was added. Choose Add a server to try again.\"",
              "You closed or cancelled Discord's confirm screen.",
              "Choose \"Add a server\" again and choose \"Authorize\" on Discord's screen."
            ],
            [
              "\"Discord did not list a server you can add a bot to. You need to own the server, or have Manage Server on it.\"",
              "The Discord account you used has no server it can add Bagel to.",
              "Use the Discord account that owns the server or has Manage Server on it."
            ],
            [
              "\"No server we can add Bagel to\"",
              "The server list is empty: \"Bagel can only be added to a server you own, or one where you have Manage Server.\"",
              "Ask a server owner for Manage Server, or choose \"Create my Discord\" to start from Bagel's template."
            ],
            [
              "\"Linked to another channel\"",
              "Another Twitch channel already uses this server.",
              "Ask that channel's owner to choose \"Disconnect\" on the server's Settings page in Bagel. Do not remove Bagel from the server."
            ],
            [
              "\"That Discord server is already linked to another Twitch channel. Ask that channel's owner to open this server in Bagel, go to Settings and choose Disconnect. Then add it here again.\"",
              "You authorized Bagel on a server that belongs to another channel's setup.",
              "Ask that owner to choose \"Disconnect\", then add the server again."
            ],
            [
              "\"Discord refused that because Bagel is missing a permission in this server. In Discord, open Server Settings, then Roles, and drag Bagel's role above the roles it manages. If you unchecked a permission when adding Bagel, choose Re-authorize on this server's Settings page and keep them all checked.\"",
              "Bagel's role sits too low, or a permission was unchecked.",
              "Follow \"Fix the role order\" above. If you unchecked a permission, choose \"Re-authorize\" and keep every box checked."
            ],
            [
              "\"This server was added before Bagel asked for the Change Nickname permission, so the premium name cannot be applied here. The premium avatar still works. Choose Re-authorize Bagel, then choose Add Bagel again next to this server.\"",
              "The server was added with an older permission list.",
              "Choose \"Re-authorize Bagel\", then \"Add Bagel again\" next to the server, and keep every permission checked."
            ],
            [
              "\"Bagel needs to be added again to work in this server.\"",
              "Bagel is not in the server any more, or needs a fresh authorization.",
              "Choose \"Add Bagel again\" on the server list."
            ],
            [
              "\"Discord did not return a server. Try adding Bagel again.\"",
              "Discord ended the connection without naming a server.",
              "Choose \"Add a server\" and start again."
            ],
            [
              "\"That Discord link expired. Start from Add Bagel again.\"",
              "The page waited too long, or came from an old tab.",
              "Open \"Discord\" and choose \"Add a server\" again."
            ],
            [
              "\"Could not save the connected server. Try again.\"",
              "Bagel could not store the link to your server.",
              "Choose \"Add a server\" and try again."
            ],
            [
              "\"Someone else saved this server first. Reload and try again.\"",
              "Two people changed the same server at once.",
              "Choose \"Reload\", then make your change again."
            ],
            [
              "\"Discord is rate limiting us. Try again in a minute.\"",
              "Discord is slowing requests down.",
              "Wait a minute and try again."
            ],
            [
              "\"Discord did not answer. Try again in a moment.\"",
              "Discord did not reply in time.",
              "Wait a moment and try again."
            ],
            [
              "\"This took too long. Some of it may be missing; reload to see the rest.\"",
              "An action ran past its time limit and may be half done.",
              "Reload the page and check the result."
            ],
            [
              "\"That channel or role is gone from the server. Pick another one.\"",
              "The channel or role was deleted in Discord.",
              "Pick another one, or choose \"Create missing channels and roles\"."
            ],
            [
              "\"No Discord server is connected yet.\"",
              "This channel has no linked server, for example after \"Disconnect\".",
              "Choose \"Add a server\"."
            ],
            [
              "\"Some settings were not valid, so they were left as they were.\" or \"Saved, but these were left as they were: &#123;fields&#125;.\"",
              "Some values were rejected and kept their old value.",
              "Check the fields marked \"Not valid, so this kept the value it already had.\" and fix them."
            ],
            [
              "\"Bagel cannot post here\" or \"Bagel can no longer post in the saved channel. Give it Send Messages there, or pick another channel.\"",
              "Bagel lacks Send Messages in that channel.",
              "Give Bagel \"Send Messages\" in that channel in Discord, or pick another channel."
            ],
            [
              "\"Turn the ticket desk on and save before reposting the panel.\"",
              "You asked to repost the ticket panel while tickets are off.",
              "Turn the ticket desk on, save, then repost."
            ],
            [
              "\"Could not list your server's channels and roles, so the pickers are turned off. Nothing was changed; try again in a moment.\"",
              "Discord did not return the server's channels and roles.",
              "Wait a moment and reload the page."
            ],
            [
              "\"Could not reach the backend. Try again in a moment.\"",
              "The dashboard could not load your Discord data.",
              "Wait a moment and reload."
            ],
            [
              "\"Could not disconnect Discord.\" or \"Could not set up this server.\"",
              "The action did not finish.",
              "Try again. If it keeps failing, contact support through the Contact page (itsbagelbot.com/contact) or at support@itsbagelbot.com."
            ],
            [
              "The status \"Offline\", or \"The gateway dropped and is reconnecting on its own. Nothing to do.\"",
              "Bagel lost its connection to Discord and reconnects by itself.",
              "Wait a few minutes. If the status says \"Reauthorize\", choose \"Re-authorize Bagel\", then \"Add Bagel again\" next to the server."
            ],
            [
              "\"Something went wrong on our side. Try again, and tell support if it keeps happening.\"",
              "An unexpected error on Bagel's side.",
              "Try again, then contact support through the Contact page (itsbagelbot.com/contact) or at support@itsbagelbot.com if it repeats."
            ],
            [
              "\"Could not save Discord settings.\"",
              "The settings on the server's page did not save.",
              "Try again. If it keeps failing, reload the page and make the change again."
            ],
            [
              "\"Could not repost the ticket panel.\"",
              "Discord did not accept the ticket panel repost.",
              "Try again in a moment. Check that Bagel can post in the ticket channel."
            ],
            [
              "\"Discord rejected the bot token. It has to be replaced before the bot can come back.\"",
              "A problem on Bagel's side, shown in the server status.",
              "Nothing you can do from the dashboard. Contact support through the Contact page (itsbagelbot.com/contact) or at support@itsbagelbot.com if it lasts."
            ],
            [
              "\"This bot now needs more than one shard. Nothing you can do from here; we are on it.\"",
              "A problem on Bagel's side, shown in the server status.",
              "Nothing you can do. Wait; Bagel's team is working on it."
            ],
            [
              "\"Discord refused the gateway intents. Nothing you can do from here; we are on it.\"",
              "A problem on Bagel's side, shown in the server status.",
              "Nothing you can do. Wait; Bagel's team is working on it."
            ],
            [
              "\"Discord has not approved the privileged intents this bot needs. Nothing you can do from here; we are on it.\"",
              "A problem on Bagel's side, shown in the server status.",
              "Nothing you can do. Wait; Bagel's team is working on it."
            ],
            [
              "\"Bagel joined. This server already has a layout, so it was not rebuilt. Bagel adopted the channels it recognised; pick the rest below.\"",
              "Bagel joined and kept your existing channels.",
              "Pick the channels that are still empty on the \"Channels\" page."
            ]
          ]
        },
        {
          kind: "callout",
          tone: "note",
          html: "Last checked: 12 October 2026."
        }
      ]
    }
  ]
};

export default guide;
