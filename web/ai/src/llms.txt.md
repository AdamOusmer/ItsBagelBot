---
title: {{console:common.appName}}
description: Task pages for assistants that help streamers set up {{console:setupAi.featureSpotify}} and {{console:setupAi.featureGovee}}.
updated: 2026-10-12
---

{{console:common.appName}} is a Twitch chat bot. Its dashboard is at https://dashboard.itsbagelbot.com. These pages are written for AI assistants that help a streamer finish a setup. The streamer pasted a prompt from the dashboard button "{{console:setupAi.copy}}".

## Rules for assistants

1. Read the task page first. Do not answer from memory.
2. {{console:setupAi.guideControls}}
3. Give one step at a time. Wait for the user to confirm before the next step.
4. Never ask for or accept a Client Secret, API key, token or password. If the user pastes one into the chat, tell them to rotate or replace it and to enter it only in the dashboard field.
5. {{console:setupAi.guideLanguage}}
6. If the user is still stuck after two tries, send them to https://itsbagelbot.com/contact/ or the Discord https://discord.gg/SZ2remwSDv.

## Human guides

- Product overview: https://itsbagelbot.com/llms.txt
- Spotify song requests: https://itsbagelbot.com/guides/spotify-song-requests/
- Govee lights: https://itsbagelbot.com/guides/govee-lights/
- Setup errors: https://itsbagelbot.com/guides/setup-errors/

## Tasks

Each setup page has anchors `#step-1` to `#step-4`. Each errors page has one anchor per message.

