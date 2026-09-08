---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
heading: "Data We Collect"
plain: "Your Twitch handle, chat processed in the moment, aggregated usage stats, optional credentials, and contact email."
---

We collect only what's necessary to provide the service:

- Twitch account information (username, channel ID) provided during OAuth connection
- Chat messages processed in real-time for moderation (not stored permanently). During testing of experimental or beta features, we may temporarily collect and retain a limited sample of Twitch chat messages solely to verify feature readiness and ensure the feature functions properly. Once a feature exits beta, this temporary collection ceases immediately and no further chat messages are retained
- Usage analytics (aggregated, anonymized, no PII)
- An optional contact email address, if you choose to provide one, used for purchase receipts, gift notifications, and account-related messages. It is encrypted at rest and never used for marketing without your consent
- Optional third-party integration credentials, including API keys, client IDs, client secrets, or custom API authentication tokens (such as for Govee Lights, Spotify song requests, or custom `urlfetch` data sources). These are encrypted at rest, used strictly to execute commands or query endpoints on your behalf, never displayed back to you in plain text, and deleted when you disconnect the integration or remove the key
- Technical request and security data processed by Cloudflare, such as IP address, routing data, browser or device information, requested URL, and security events
