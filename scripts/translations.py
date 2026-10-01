#!/usr/bin/env python3
"""Dependency-free translation checks and safe, partial-locale scaffolding."""
import argparse
import json
from pathlib import Path
import re
import sys

from translation_checks import (
    MANIFEST,
    MAX_LEAVES,
    TREE,
    CheckConfig,
    check as _check,
    compare,
    content_coverage,
    flatten,
    key_prefix,
    locale_codes as _locale_codes,
    read_catalog,
    read_json,
    status,
    translation_errors,
    unique_object,
)

ROOT = Path(__file__).resolve().parents[1]
LOCALE_RE = re.compile(r'[a-z]{2,3}(?:-[a-z0-9]{2,4})?')


def locale_codes(root):
    return _locale_codes(root, MANIFEST)


def _expand_locales(root, codes):
    if 'all' not in codes:
        return tuple(codes)
    return tuple(sorted({code for code in codes if code != 'all'} |
                        {code for code in locale_codes(root) if code != 'en'}))


def check(root, strict=(), show_missing=False, strict_catalogs=()):
    return _check(CheckConfig(root, _expand_locales(root, strict), show_missing, content_coverage,
                               _expand_locales(root, strict_catalogs)))


def _validate_scaffold(code, name, og_locale):
    if not LOCALE_RE.fullmatch(code) or len(code) > 8:
        raise ValueError('Use a lowercase locale code such as es or pt-br (maximum 8 characters).')
    if not name.strip() or not re.fullmatch(r'[a-z]{2,3}_[A-Z]{2}', og_locale):
        raise ValueError('Supply the native language name and an Open Graph locale such as es_ES.')


def _required_keys(name, og_locale):
    return {
        'console': {'lang.name': name},
        'website': {'lang.name': name, 'lang.ogLocale': og_locale},
        'docs': {'lang.name': name},
    }


def _prune(tree, prefix, wanted):
    result = {}
    for key, value in tree.items():
        name = f'{prefix}.{key}' if prefix else key
        kept = _prune(value, name, wanted) if isinstance(value, dict) else wanted.get(name)
        if kept:
            result[key] = kept
    return result


def scaffold_files(root, required):
    files = {}
    for surface, wanted in required.items():
        english = read_catalog(root, 'en', surface)
        for relative in sorted({english.keys[key][1] for key in wanted if key in english.keys}):
            tree = read_json(root / english.path(relative))
            files[f'{surface}/{relative}'] = _prune(tree, key_prefix(relative), wanted)
    return files


def scaffold(root, code, name, og_locale):
    _validate_scaffold(code, name, og_locale)
    codes = locale_codes(root)
    folder = root / TREE / code
    if code in codes or folder.exists():
        raise ValueError(f'{code} already exists; no files changed. Edit its existing files in {TREE}/{code}/ instead.')
    files = scaffold_files(root, _required_keys(name, og_locale))
    for relative, content in files.items():
        path = folder / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(content, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    (root / MANIFEST).write_text(json.dumps(codes + [code], indent=2) + '\n', encoding='utf-8')
    print(f'Created {TREE}/{code}/ with {len(files)} starter files and registered the locale. Missing text falls back to English.')
    print(f'Translate files from {TREE}/en/ into the same paths under {TREE}/{code}/. '
          'Leave out text you have not translated yet instead of copying English.')
    print(f'Run python3 scripts/translations.py status {code} to see what is left. '
          'Guides, legal pages and documentation pages live elsewhere; see TRANSLATIONS.md.')


def _parser():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    audit = commands.add_parser('check', help='validate catalogs and report coverage')
    audit.add_argument('--strict', action='append', default=[], metavar='LOCALE', help='require every English key, docs pages, and legal documents for this locale; repeatable, or pass "all" for every non-English locale in the manifest')
    audit.add_argument('--strict-catalogs', action='append', default=[], metavar='LOCALE', help='require every English key (all surfaces, including ui) for this locale, without requiring docs pages or legal documents; repeatable, or pass "all" for every non-English locale in the manifest')
    audit.add_argument('--missing', action='store_true', help='list missing keys to translate')
    progress = commands.add_parser('status', help='list missing keys for one language, grouped by file')
    progress.add_argument('locale')
    progress.add_argument('--keys', action='store_true', help='also list each missing key under its file')
    new = commands.add_parser('new', help='create a partial language without overwriting existing work')
    new.add_argument('locale')
    new.add_argument('name', nargs='?', help='native language name, e.g. Español')
    new.add_argument('og_locale', nargs='?', metavar='og-locale', help='social-card locale, e.g. es_ES')
    new.add_argument('--name', dest='name_option', help=argparse.SUPPRESS)
    new.add_argument('--og-locale', dest='og_locale_option', help=argparse.SUPPRESS)
    return parser


def scaffold_arguments(args):
    return args.locale, args.name or args.name_option or '', args.og_locale or args.og_locale_option or ''


def main():
    args = _parser().parse_args()
    try:
        if args.command == 'check':
            return check(ROOT, args.strict, args.missing, args.strict_catalogs)
        if args.command == 'status':
            return status(ROOT, args.locale, args.keys)
        scaffold(ROOT, *scaffold_arguments(args))
        return 0
    except ValueError as exc:
        print(f'ERROR {exc}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
