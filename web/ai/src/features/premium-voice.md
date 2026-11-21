---
title: Premium voice alerts in OBS (Beta)
description: Premium access, monthly speech allowances, OBS browser source setup, alert settings, and audio troubleshooting.
updated: 2026-11-20
kind: feature
human: https://itsbagelbot.com/guides/premium-voice/
---

[Setup guide](https://itsbagelbot.com/guides/premium-voice/).

Premium voice is in beta. It uses cloud voices to read Twitch alerts aloud through an OBS Browser Source.

<page type="feature">

<audience>

Assistants answering Premium voice questions or helping channel owners configure spoken alerts in OBS.

</audience>

<assistant_rules>

- State the Premium access requirement and the need for an OBS Browser Source before suggesting a test.
- Keep the full OBS URL out of the conversation. Direct the owner to copy it from the dashboard into OBS. If it was shared, direct the owner to create a replacement in the dashboard and update OBS.
- Link to the setup guide when explaining setup or troubleshooting. Give one step at a time only while actively guiding setup.
- {{console:setupAi.guideLanguage}}

</assistant_rules>

<facts>

- The channel owner manages speech on [{{console:nav.voice}}](https://dashboard.itsbagelbot.com/voice).
- Monthly speech allowances are 60 minutes for Premium, 60 for Giveaway Premium, 30 for an admin grant, and 120 for VIP. The dashboard shows usage and the reset date. Cloud speech pauses when the allowance is used up.
- The supported events are follows, subscriptions, gifted subscriptions, Cheers, and raids. Follow speech is off by default. The other four alert types are on by default, subject to the master switch and eligibility.
- Each alert can have a spoken template, a style, and a voice override. The styles are warm, hype, calm, playful, and cozy. Each alert uses the default voice unless an override is selected.
- Live-only playback and reading viewer messages from Cheers and resubs are on by default. The default minimum Cheer is 100 Bits.
- Max length per alert can be set from 5 to 120 seconds. Shorter messages are less likely to be cut off. Remaining monthly minutes can also shorten an alert.

</facts>

## Set up and test

1. Sign in as the channel owner, open [{{console:nav.voice}}](https://dashboard.itsbagelbot.com/voice), enable **{{console:voice.enable}}**, and save.
2. In **{{console:voice.overlayTitle}}**, choose **{{console:voice.overlayCreate}}** and copy the full link, including the part after `#`. The dashboard shows the link once.
3. In OBS, add a Browser Source to the scene, paste the link into **URL**, and use a small size such as 320 × 80. Tick **Control audio via OBS**.
4. Choose the default voice and decide which event types speak. Pick their styles, edit the spoken messages, and save.
5. With the OBS scene active, trigger an enabled alert and check the Audio Mixer. If live-only is on, run the test while the channel is live.

## Troubleshooting

- If an alert is silent, check **Control audio via OBS** and the source’s mute setting in the Audio Mixer. If autoplay is blocked, right-click the source, choose **Interact**, and select **Click to enable sound** when it appears.
- **{{console:voice.overlayReplace}}** revokes the old URL. Replace it in each OBS source that used it.
- If the channel runs out of minutes, check the reset date on the Premium voice page.
- If speech stops early, shorten the spoken message or increase the maximum alert length.

<related>

- [Premium voice setup guide](https://itsbagelbot.com/guides/premium-voice/).
- [Product and plan reference](../product.md).

</related>

</page>
