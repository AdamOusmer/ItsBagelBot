// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The one Minecraft identity this product answers for.
 *
 * Hypixel verifies that a product belongs to an account by looking for the
 * username as plain, visible text on the site (their reviewers and automated
 * checks read served HTML, not repo source). Every surface that names the
 * owner — the Games legal line, the About colophon, the JSON-LD founder in
 * Layout — must spell it identically, so it lives here and nowhere else.
 * Changing the IGN here changes it everywhere at once; changing it anywhere
 * else desynchronizes the ownership proof.
 */
export const OWNER_IGN = 'ItsMavey';

/** Public registry page proving the account exists; linked wherever OWNER_IGN appears. */
export const OWNER_PROFILE_URL = `https://namemc.com/profile/${OWNER_IGN}`;
