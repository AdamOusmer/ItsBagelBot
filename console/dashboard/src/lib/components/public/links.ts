// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Link helpers for the public (signed-out) pages. Those pages wear the
// marketing site's nav + footer, so every chrome link points at the live
// marketing origin rather than at a dashboard route — and follows the visitor
// into their language the same way web/src/i18n/ui.ts localizePath() does.
import { DEFAULT_LOCALE, type Locale } from '@bagel/shared/i18n';

/** The live marketing site. */
export const WEB = 'https://itsbagelbot.com';
/** This app's own public origin, used by the footer Dashboard link. */
export const DASH = 'https://dashboard.itsbagelbot.com';
/** OAuth start. Add to Twitch hits this so Twitch redirects into the console. */
export const DASH_LOGIN = `${DASH}/auth/login`;
/** The short host the bot hands out for a channel's public command page. */
export const COMMANDS = 'https://commands.itsbagelbot.com';

/** One entry in the public nav's link row. */
export interface PublicNavLink {
  href: string;
  label: string;
  /** Lit + aria-current, for a link that points at the page you are on. */
  active?: boolean;
}

/**
 * A marketing-site URL for `path`, in the visitor's language. Mirrors web's
 * localizePath(): the default locale keeps the bare path, any other locale
 * takes a /<locale> prefix.
 */
export function webHref(path: string, locale: Locale): string {
  return locale === DEFAULT_LOCALE ? `${WEB}${path}` : `${WEB}/${locale}${path}`;
}

/** The marketing home page, localized (web renders '/fr/' with the slash). */
export function webHome(locale: Locale): string {
  return locale === DEFAULT_LOCALE ? WEB : `${WEB}/${locale}/`;
}

/**
 * A channel's public command page, always absolute.
 *
 * The app answers /user/<login> on every hostname it serves, so a RELATIVE link
 * keeps the visitor on whichever host they were already on. That is how
 * leaderboard.itsbagelbot.com/user/<login> came to exist and serve the commands
 * page under the leaderboard origin: the board page linked here relatively. The
 * server 308s that back to the canonical host now, so this helper is about not
 * making every visitor pay for the redirect.
 *
 * Takes the raw URL segment, not a login, because the stats boards link some
 * channels by id when their display name cannot be one.
 */
export function commandsHref(segment: string): string {
  return `${COMMANDS}/user/${segment}`;
}

/**
 * The dashboard origin. The dashboard has no /<locale> routes — it reads
 * ?lang= — so a non-default locale rides over as a query. Footer "Dashboard"
 * uses this; Add to Twitch uses dashLoginHref so the click starts OAuth.
 */
export function dashHref(locale: Locale): string {
  return locale === DEFAULT_LOCALE ? DASH : `${DASH}?lang=${locale}`;
}

export function dashLoginHref(locale: Locale): string {
  return locale === DEFAULT_LOCALE ? DASH_LOGIN : `${DASH_LOGIN}?lang=${locale}`;
}
