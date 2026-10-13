// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: "share-access",
  meta: {
    title: "Share your dashboard with a mod - ItsBagelBot Guides",
    description: "How to give a mod access to your ItsBagelBot dashboard: create a one-use share link, choose the sections, what Billing allows, revoke access, and the messages you might see.",
    eyebrow: "Guide",
    heading: "Share your dashboard with a mod",
    lead: "Let a mod manage parts of your dashboard from their own Twitch account, and take the access back whenever you want.",
    minutes: "5 min read",
    card: {
      title: "Share your dashboard",
      description: "Create a one-use share link, choose what the mod can open, and revoke access.",
      meta: "5 min · 3 steps",
      chips: [
        "share link",
        "sections",
        "Billing",
        "revoke"
      ]
    }
  },
  sections: [
    {
      id: "need",
      heading: "What you need",
      note: "Your dashboard, and the mod's own Twitch account.",
      blocks: [
        {
          kind: "prose",
          html: "<p>You need to be signed in to your own dashboard, and the mod needs their own Twitch account. Sharing is done from \"Settings\", in the \"Shared access\" section. Only you can use that section: mods never see it.</p>"
        }
      ]
    },
    {
      id: "create",
      heading: "Create a share link",
      note: "Each link works once. The first Twitch account that opens it gets access.",
      blocks: [
        {
          kind: "steps",
          items: [
            {
              title: "Open \"Settings\"",
              html: "<p>In the dashboard menu, open \"Settings\", then go to \"Shared access\".</p>"
            },
            {
              title: "Choose \"New share link\"",
              html: "<p>The form opens under \"Sections to share\".</p>"
            },
            {
              title: "Pick the sections",
              html: "<p>Tick each section the mod should manage. \"Commands\" is ticked at first. Pick at least one, or the form shows \"Pick at least one section.\"</p>"
            },
            {
              title: "Choose \"Generate link\"",
              html: "<p>Bagel shows \"Share link created. Copy it below.\" The link now waits under \"Invite links\".</p>"
            },
            {
              title: "Choose \"Copy\" and send the link to the mod",
              html: "<p>Send it only to the mod it is for, in a private message.</p>"
            }
          ]
        },
        {
          kind: "callout",
          tone: "warn",
          html: "<b>One link, one account</b> Each link works once: the first Twitch account that opens it gets access, whoever that is. Send it only to the mod. If the wrong account opens it, see \"Change or revoke access\" below."
        }
      ]
    },
    {
      id: "sections",
      heading: "What each section grants",
      note: "Read this before you tick Billing.",
      blocks: [
        {
          kind: "table",
          head: [
            "Section",
            "What the mod can open"
          ],
          rows: [
            [
              "Commands",
              "The \"Commands\" page. It also opens \"Quotes\" and \"Timers\"."
            ],
            [
              "Modules",
              "The \"Modules\" page and the module pages you can open from it."
            ],
            [
              "Discord",
              "The \"Discord\" page. Discord is a Premium beta module."
            ],
            [
              "Channel Points",
              "The \"Channel Points\" page."
            ],
            [
              "Billing",
              "The \"Billing\" page. With Billing, the mod can buy Premium for your channel and open its subscription management."
            ]
          ]
        },
        {
          kind: "callout",
          tone: "warn",
          html: "<b>Billing spends your money</b> A mod with Billing can buy Premium for your channel and open its subscription management, where payments are reviewed and the subscription can be changed. Tick Billing only for someone you trust with that."
        },
        {
          kind: "prose",
          html: "<p>What is not shared: \"Settings\" is hidden from mods, so they cannot create share links, and deleting the account is blocked for them. Secrets saved for data sources can be used by a mod but never read.</p>"
        }
      ]
    },
    {
      id: "mod",
      heading: "What the mod does",
      note: "Open the link and sign in with Twitch.",
      blocks: [
        {
          kind: "steps",
          items: [
            {
              title: "Open the link",
              html: "<p>The mod opens the link you sent in their browser.</p>"
            },
            {
              title: "Sign in with Twitch",
              html: "<p>Twitch asks them to sign in. They must use their own Twitch account, not yours.</p>"
            },
            {
              title: "Work in your dashboard",
              html: "<p>The mod lands in your dashboard and sees only the sections you shared.</p>"
            }
          ]
        },
        {
          kind: "prose",
          html: "<p>A banner at the top reads \"Shared access to your_name's dashboard\" and lists the sections. \"Exit\" returns the mod to their own account. Later, they find your dashboard on their own \"Settings\" page under \"Dashboards shared with you\": \"Open\" goes in, and \"Leave\" gives the access up.</p>"
        }
      ]
    },
    {
      id: "revoke",
      heading: "Change or revoke access",
      note: "Everything lives under \"Shared access\" in \"Settings\".",
      blocks: [
        {
          kind: "prose",
          html: "<p>A mod who has opened a link appears under \"In use\", with their Twitch name and the sections they hold. Links nobody has opened yet wait under \"Invite links\".</p>"
        },
        {
          kind: "steps",
          items: [
            {
              title: "Change the sections",
              html: "<p>Choose \"Edit access\" next to the mod or the link, change the ticks, then choose \"Save\".</p>"
            },
            {
              title: "Revoke a mod or a link",
              html: "<p>Choose \"Revoke\", then confirm \"Revoke this link?\" with \"Revoke\". For a mod, they lose access to your dashboard immediately. For a link nobody opened, it stops working immediately. This cannot be undone.</p>"
            }
          ]
        },
        {
          kind: "callout",
          tone: "tip",
          html: "<b>The wrong account opened the link</b> That account shows under \"In use\". Choose \"Revoke\" there, then create a new link with \"New share link\" and send it to the right mod."
        }
      ]
    },
    {
      id: "trouble",
      heading: "If something goes wrong",
      note: "The messages you or your mod might see, and what to do.",
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
              "\"That share link is no longer valid. Ask the broadcaster for a new one.\" (sign-in page)",
              "The link was already used, was revoked, or is incomplete.",
              "Mod: ask the broadcaster for a new link. Owner: check \"Shared access\", then create a new link with \"New share link\"."
            ],
            [
              "\"Sign-in did not finish on the server. Nothing was saved. Please try again.\" (sign-in page)",
              "The sign-in stopped before the link was accepted.",
              "Open the share link again. If it then shows the \"no longer valid\" message, ask for a new link."
            ],
            [
              "\"That dashboard is no longer shared with you. Ask the owner to send you a new share link.\" (Settings)",
              "The owner revoked your access, or it ended.",
              "Ask the owner for a new share link."
            ],
            [
              "\"Could not open that dashboard right now. Wait a moment, then choose Open again.\" (Settings)",
              "Bagel could not check your access at that moment.",
              "Wait a moment, then choose \"Open\" again."
            ],
            [
              "\"That share link is yours, so it opened your own dashboard. It still works. Send it to your mod to open while signed in to their own Twitch account.\" (Settings)",
              "You opened your own link while signed in as the owner. Nothing was used up.",
              "Send the same link to the mod. They open it signed in to their own Twitch account."
            ],
            [
              "\"Premium for this channel is managed by &#123;login&#125;. Ask them to upgrade on their Billing page.\"",
              "You are a mod, a Premium beta page needs Premium, and Billing is not shared with you. &#123;login&#125; is the owner.",
              "Ask &#123;login&#125; to upgrade on their Billing page. Owners can also tick \"Billing\" under \"Edit access\"."
            ],
            [
              "\"You do not have access to manage billing.\"",
              "Billing is not part of what was shared with you.",
              "Ask the owner to add \"Billing\" under \"Edit access\", or to handle it."
            ],
            [
              "\"Only the account owner can do that.\"",
              "That action is reserved for the owner.",
              "Ask the owner to do it."
            ],
            [
              "\"Pick at least one section.\"",
              "The form was sent with every section unticked.",
              "Tick at least one section, then choose \"Generate link\" or \"Save\"."
            ],
            [
              "\"Clipboard blocked. Select the link manually.\"",
              "Your browser blocked \"Copy\".",
              "Select the link under \"Invite links\" and copy it yourself."
            ],
            [
              "\"Could not create link.\" or \"Could not update link.\" or \"Could not revoke link.\"",
              "The action did not finish.",
              "Try again in a moment. Check \"Shared access\" to see what was saved."
            ],
            [
              "\"Could not leave dashboard.\"",
              "Leaving did not finish.",
              "Try \"Leave\" again in a moment."
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
