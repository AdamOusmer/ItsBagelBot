// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// HTML entity decoder for the Wizebot import source.
//
// Why this file exists: Wizebot's streaming website renders its command list
// into an HTML page, so every string the JSON list carries is HTML-escaped at
// the SOURCE level, not at the transport level. A French channel's response
// arrives as "Retrouves la cha&icirc;ne &amp; le Discord" and its command name
// as "!d&eacute;gage". Nothing in shared/lib decoded entities before (no other
// import source emits them), and the runtime cannot borrow the browser's
// decoder: parsing happens server-side in the dashboard pod, where there is no
// DOM, and building one out of innerHTML in the browser half would be an
// injection surface for text we are about to store.
//
// Table scope (deliberate, decision record): the HTML 4.01 Latin-1 block
// (&nbsp; through &yuml;), the Latin Extended-A pair Wizebot's French/Polish
// channels emit (&OElig; &oelig; &Scaron; &scaron; &Yuml; &fnof;), the common
// typographic set (&ndash; &mdash; quotes, &hellip;, &bull;, &dagger;,
// &permil;, &euro;, arrows) and the five XML predefined names. That is what a
// chat message realistically holds. The HTML5 named-reference table is ~2200
// entries; shipping it would multiply this module's weight for entities
// (&angmsdaw;, &boxDR;) that no Twitch command has ever contained. An unknown
// NAMED entity is therefore left EXACTLY as written rather than dropped: the
// broadcaster sees "&curren;" in the review step and can fix it, which is
// strictly better than eating a character we failed to recognize.
//
// Numeric references (&#39; and &#x27;) are decoded generically, so the long
// tail of "some emoji as a code point" needs no table entry at all.

// NAMED is keyed WITHOUT the ampersand and semicolon. Case matters: &Eacute;
// and &eacute; are different characters, and Wizebot emits both.
const NAMED: Record<string, string> = {
  // XML predefined + the ampersand forms every escaper produces.
  amp: '&',
  lt: '<',
  gt: '>',
  quot: '"',
  apos: "'",
  // Latin-1 punctuation and symbols.
  nbsp: ' ',
  iexcl: '¡',
  cent: '¢',
  pound: '£',
  curren: '¤',
  yen: '¥',
  brvbar: '¦',
  sect: '§',
  uml: '¨',
  copy: '©',
  ordf: 'ª',
  laquo: '«',
  not: '¬',
  shy: '­',
  reg: '®',
  macr: '¯',
  deg: '°',
  plusmn: '±',
  sup2: '²',
  sup3: '³',
  acute: '´',
  micro: 'µ',
  para: '¶',
  middot: '·',
  cedil: '¸',
  sup1: '¹',
  ordm: 'º',
  raquo: '»',
  frac14: '¼',
  frac12: '½',
  frac34: '¾',
  iquest: '¿',
  times: '×',
  divide: '÷',
  // Latin-1 letters.
  Agrave: 'À',
  Aacute: 'Á',
  Acirc: 'Â',
  Atilde: 'Ã',
  Auml: 'Ä',
  Aring: 'Å',
  AElig: 'Æ',
  Ccedil: 'Ç',
  Egrave: 'È',
  Eacute: 'É',
  Ecirc: 'Ê',
  Euml: 'Ë',
  Igrave: 'Ì',
  Iacute: 'Í',
  Icirc: 'Î',
  Iuml: 'Ï',
  ETH: 'Ð',
  Ntilde: 'Ñ',
  Ograve: 'Ò',
  Oacute: 'Ó',
  Ocirc: 'Ô',
  Otilde: 'Õ',
  Ouml: 'Ö',
  Oslash: 'Ø',
  Ugrave: 'Ù',
  Uacute: 'Ú',
  Ucirc: 'Û',
  Uuml: 'Ü',
  Yacute: 'Ý',
  THORN: 'Þ',
  szlig: 'ß',
  agrave: 'à',
  aacute: 'á',
  acirc: 'â',
  atilde: 'ã',
  auml: 'ä',
  aring: 'å',
  aelig: 'æ',
  ccedil: 'ç',
  egrave: 'è',
  eacute: 'é',
  ecirc: 'ê',
  euml: 'ë',
  igrave: 'ì',
  iacute: 'í',
  icirc: 'î',
  iuml: 'ï',
  eth: 'ð',
  ntilde: 'ñ',
  ograve: 'ò',
  oacute: 'ó',
  ocirc: 'ô',
  otilde: 'õ',
  ouml: 'ö',
  oslash: 'ø',
  ugrave: 'ù',
  uacute: 'ú',
  ucirc: 'û',
  uuml: 'ü',
  yacute: 'ý',
  thorn: 'þ',
  yuml: 'ÿ',
  // Latin Extended-A / letterlike, as emitted by non-French channels.
  OElig: 'Œ',
  oelig: 'œ',
  Scaron: 'Š',
  scaron: 'š',
  Yuml: 'Ÿ',
  fnof: 'ƒ',
  // Typographic set.
  ensp: ' ',
  emsp: ' ',
  thinsp: ' ',
  ndash: '–',
  mdash: '—',
  lsquo: '‘',
  rsquo: '’',
  sbquo: '‚',
  ldquo: '“',
  rdquo: '”',
  bdquo: '„',
  dagger: '†',
  Dagger: '‡',
  bull: '•',
  hellip: '…',
  permil: '‰',
  prime: '′',
  Prime: '″',
  lsaquo: '‹',
  rsaquo: '›',
  oline: '‾',
  frasl: '⁄',
  euro: '€',
  trade: '™',
  larr: '←',
  uarr: '↑',
  rarr: '→',
  darr: '↓',
  harr: '↔'
};

// REFERENCE matches one entity reference: a numeric body (#39, #x27) or a
// bare name. The name class stays [A-Za-z][A-Za-z0-9]* so a stray ampersand in
// running text ("Twitch & Youtube: ...") never starts a match it cannot finish.
const REFERENCE = /&(#[Xx]?[0-9A-Fa-f]+|[A-Za-z][A-Za-z0-9]*);/g;

// MAX_CODE_POINT is Unicode's ceiling. Surrogate halves are refused with it:
// String.fromCodePoint would happily build a lone surrogate, which is not
// valid UTF-8 on the wire and would be replaced with U+FFFD by the encoder
// later, one layer too far away to explain itself.
const MAX_CODE_POINT = 0x10ffff;

function isUsableCodePoint(code: number): boolean {
  if (!Number.isInteger(code)) return false;
  if (code < 1 || code > MAX_CODE_POINT) return false;
  return code < 0xd800 || code > 0xdfff;
}

// numericChar decodes "#39" / "#x27" bodies. An out-of-range or malformed
// reference falls back to the literal text it was written as.
function numericChar(raw: string, body: string): string {
  const hex = body[1] === 'x' || body[1] === 'X';
  const digits = hex ? body.slice(2) : body.slice(1);
  const code = Number.parseInt(digits, hex ? 16 : 10);
  return isUsableCodePoint(code) ? String.fromCodePoint(code) : raw;
}

// decodeEntities turns one HTML-escaped Wizebot string back into the text a
// viewer sees in chat. Unknown named references survive verbatim (see the
// table scope above), so this function never loses a character.
export function decodeEntities(input: string): string {
  if (!input.includes('&')) return input;
  return input.replace(REFERENCE, (raw: string, body: string) =>
    body.startsWith('#') ? numericChar(raw, body) : (NAMED[body] ?? raw)
  );
}
