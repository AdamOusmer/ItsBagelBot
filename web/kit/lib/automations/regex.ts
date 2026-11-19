// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const LOOKAROUND = /\(\?<?[=!]/;
const BACKREFERENCE = /\\[1-9]|\\k</;
const GO_ONLY_ESCAPE_REJECTED = /\\[CFGHIJKLMNORTUVXYZcegijklmoquy]/g;
const FLAG_GROUP = /\(\?[imsU]+\)/g;
const FLAG_SCOPE = /\(\?[imsU-]+:/g;
const NAMED_GROUP = /\(\?P</g;
const REPEAT_COUNT = /\{(\d+)(?:,(\d*))?\}/y;
const MAX_REPEAT = 1000;
const ESCAPED_TOKEN = /\\(?:Q[\s\S]*?(?:\\E|$)|[pP](?:\{([^}]*)\}|([^{]))?|[\s\S])/gu;

const GO_UNICODE_CLASSES = new Set([
  'Adlam', 'Ahom', 'Anatolian_Hieroglyphs', 'Any', 'Arabic', 'Armenian', 'ASCII', 'Assigned',
  'Avestan', 'Balinese', 'Bamum', 'Bassa_Vah', 'Batak', 'Bengali', 'Beria_Erfe', 'Bhaiksuki',
  'Bopomofo', 'Brahmi', 'Braille', 'Buginese', 'Buhid', 'C', 'Canadian_Aboriginal', 'Carian',
  'Cased_Letter', 'Caucasian_Albanian', 'Cc', 'Cf', 'Chakma', 'Cham', 'Cherokee', 'Chorasmian',
  'Close_Punctuation', 'Cn', 'cntrl', 'Co', 'Combining_Mark', 'Common', 'Connector_Punctuation', 'Control',
  'Coptic', 'Cs', 'Cuneiform', 'Currency_Symbol', 'Cypriot', 'Cypro_Minoan', 'Cyrillic', 'Dash_Punctuation',
  'Decimal_Number', 'Deseret', 'Devanagari', 'digit', 'Dives_Akuru', 'Dogra', 'Duployan', 'Egyptian_Hieroglyphs',
  'Elbasan', 'Elymaic', 'Enclosing_Mark', 'Ethiopic', 'Final_Punctuation', 'Format', 'Garay', 'Georgian',
  'Glagolitic', 'Gothic', 'Grantha', 'Greek', 'Gujarati', 'Gunjala_Gondi', 'Gurmukhi', 'Gurung_Khema',
  'Han', 'Hangul', 'Hanifi_Rohingya', 'Hanunoo', 'Hatran', 'Hebrew', 'Hiragana', 'Imperial_Aramaic',
  'Inherited', 'Initial_Punctuation', 'Inscriptional_Pahlavi', 'Inscriptional_Parthian', 'Javanese', 'Kaithi', 'Kannada', 'Katakana',
  'Kawi', 'Kayah_Li', 'Kharoshthi', 'Khitan_Small_Script', 'Khmer', 'Khojki', 'Khudawadi', 'Kirat_Rai',
  'L', 'Lao', 'Latin', 'LC', 'Lepcha', 'Letter', 'Letter_Number', 'Limbu',
  'Line_Separator', 'Linear_A', 'Linear_B', 'Lisu', 'Ll', 'Lm', 'Lo', 'Lowercase_Letter',
  'Lt', 'Lu', 'Lycian', 'Lydian', 'M', 'Mahajani', 'Makasar', 'Malayalam',
  'Mandaic', 'Manichaean', 'Marchen', 'Mark', 'Masaram_Gondi', 'Math_Symbol', 'Mc', 'Me',
  'Medefaidrin', 'Meetei_Mayek', 'Mende_Kikakui', 'Meroitic_Cursive', 'Meroitic_Hieroglyphs', 'Miao', 'Mn', 'Modi',
  'Modifier_Letter', 'Modifier_Symbol', 'Mongolian', 'Mro', 'Multani', 'Myanmar', 'N', 'Nabataean',
  'Nag_Mundari', 'Nandinagari', 'Nd', 'New_Tai_Lue', 'Newa', 'Nko', 'Nl', 'No',
  'Nonspacing_Mark', 'Number', 'Nushu', 'Nyiakeng_Puachue_Hmong', 'Ogham', 'Ol_Chiki', 'Ol_Onal', 'Old_Hungarian',
  'Old_Italic', 'Old_North_Arabian', 'Old_Permic', 'Old_Persian', 'Old_Sogdian', 'Old_South_Arabian', 'Old_Turkic', 'Old_Uyghur',
  'Open_Punctuation', 'Oriya', 'Osage', 'Osmanya', 'Other', 'Other_Letter', 'Other_Number', 'Other_Punctuation',
  'Other_Symbol', 'P', 'Pahawh_Hmong', 'Palmyrene', 'Paragraph_Separator', 'Pau_Cin_Hau', 'Pc', 'Pd',
  'Pe', 'Pf', 'Phags_Pa', 'Phoenician', 'Pi', 'Po', 'Private_Use', 'Ps',
  'Psalter_Pahlavi', 'punct', 'Punctuation', 'Rejang', 'Runic', 'S', 'Samaritan', 'Saurashtra',
  'Sc', 'Separator', 'Sharada', 'Shavian', 'Siddham', 'Sidetic', 'SignWriting', 'Sinhala',
  'Sk', 'Sm', 'So', 'Sogdian', 'Sora_Sompeng', 'Soyombo', 'Space_Separator', 'Spacing_Mark',
  'Sundanese', 'Sunuwar', 'Surrogate', 'Syloti_Nagri', 'Symbol', 'Syriac', 'Tagalog', 'Tagbanwa',
  'Tai_Le', 'Tai_Tham', 'Tai_Viet', 'Tai_Yo', 'Takri', 'Tamil', 'Tangsa', 'Tangut',
  'Telugu', 'Thaana', 'Thai', 'Tibetan', 'Tifinagh', 'Tirhuta', 'Titlecase_Letter', 'Todhri',
  'Tolong_Siki', 'Toto', 'Tulu_Tigalari', 'Ugaritic', 'Unassigned', 'Uppercase_Letter', 'Vai', 'Vithkuqi',
  'Wancho', 'Warang_Citi', 'Yezidi', 'Yi', 'Z', 'Zanabazar_Square', 'Zl', 'Zp', 'Zs',
].map(canonicalUnicodeClass));

const UNSUPPORTED = [LOOKAROUND, BACKREFERENCE];

type RegexSource = string;

type Cursor = { source: RegexSource; at: number };

type ScanKind = 'open' | 'close' | 'atom' | 'modifier' | 'repeat';

type Scan = { end: number; kind: ScanKind; factor: number };

const SINGLE_CHARS: Readonly<Record<string, ScanKind>> = { '(': 'open', ')': 'close', '*': 'modifier', '+': 'modifier', '?': 'modifier' };

const atomUntil = (end: number): Scan => ({ end, kind: 'atom', factor: 1 });

function classEnd({ source, at }: Cursor): number {
  let i = at + 1;
  if (source[i] === '^') i++;
  if (source[i] === ']') i++;
  while (i < source.length && source[i] !== ']') i += source[i] === '\\' ? 2 : 1;
  return i + 1;
}

function repeatFactor(m: RegExpExecArray): number {
  return Math.max(Number(m[2] || m[1]), 1);
}

function repeatAt({ source, at }: Cursor): Scan | null {
  REPEAT_COUNT.lastIndex = at;
  const m = REPEAT_COUNT.exec(source);
  return m && { end: REPEAT_COUNT.lastIndex, kind: 'repeat', factor: repeatFactor(m) };
}

const SCANNERS: Readonly<Record<string, (c: Cursor) => Scan>> = {
  '\\': (c) => atomUntil(c.at + 2),
  '[': (c) => atomUntil(classEnd(c)),
  '{': (c) => repeatAt(c) ?? atomUntil(c.at + 1)
};

function scanAt(c: Cursor): Scan {
  const char = c.source[c.at]!;
  if (Object.hasOwn(SCANNERS, char)) return SCANNERS[char]!(c);
  return { end: c.at + 1, kind: Object.hasOwn(SINGLE_CHARS, char) ? SINGLE_CHARS[char]! : 'atom', factor: 1 };
}

type RepeatDepth = { parents: number[]; level: number; atom: number };

const DEPTH_STEPS: Readonly<Record<ScanKind, (d: RepeatDepth, scan: Scan) => void>> = {
  open: (d) => {
    d.parents.push(d.level);
    d.level = 1;
  },
  close: (d) => {
    d.atom = d.level;
    d.level = Math.max(d.parents.pop() ?? 1, d.atom);
  },
  atom: (d) => {
    d.atom = 1;
  },
  modifier: () => {},
  repeat: (d, scan) => {
    d.atom *= scan.factor;
    d.level = Math.max(d.level, d.atom);
  }
};

// Go rejects a repeat whose product with every enclosing repeat exceeds 1000.
function repeatTooLarge(source: RegexSource): boolean {
  const depth: RepeatDepth = { parents: [], level: 1, atom: 1 };
  for (let at = 0; at < source.length && depth.atom <= MAX_REPEAT; ) {
    const scan = scanAt({ source, at });
    at = scan.end;
    DEPTH_STEPS[scan.kind](depth, scan);
  }
  return depth.atom > MAX_REPEAT;
}

function hasGoRejectedEscape(source: RegexSource): boolean {
  return source.replace(/\\\\/g, '').search(GO_ONLY_ESCAPE_REJECTED) >= 0;
}

function hasUnsupportedSyntax(source: RegexSource): boolean {
  return UNSUPPORTED.some((syntax) => syntax.test(source)) || hasGoRejectedEscape(source);
}

function canonicalUnicodeClass(name: RegexSource): RegexSource {
  return name.replace(/[ _-]/g, '').replace(/[A-Z]/g, (c) => c.toLowerCase());
}

function normalizeUnicodeClasses(source: RegexSource): RegexSource | null {
  let valid = true;
  const normalized = source.replace(ESCAPED_TOKEN, (token: RegexSource, braced?: RegexSource, single?: RegexSource) => {
    if (token[1] !== 'p' && token[1] !== 'P') return token;
    const name = braced?.replace(/^\^/, '') ?? single ?? '';
    if (!GO_UNICODE_CLASSES.has(canonicalUnicodeClass(name))) valid = false;
    return '\\w';
  });
  return valid ? normalized : null;
}

function toJsSyntax(source: RegexSource): RegexSource {
  return source.replace(FLAG_GROUP, '').replace(FLAG_SCOPE, '(?:').replace(NAMED_GROUP, '(?<');
}

function goRejects(normalized: RegexSource): boolean {
  return hasUnsupportedSyntax(normalized) || repeatTooLarge(normalized);
}

export function compilesAsRe2(pattern: RegexSource): boolean {
  const normalized = normalizeUnicodeClasses(pattern);
  if (normalized === null || goRejects(normalized)) return false;
  try {
    new RegExp(toJsSyntax(normalized));
    return true;
  } catch {
    return false;
  }
}
