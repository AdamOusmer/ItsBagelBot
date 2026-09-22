#!/usr/bin/env python3
"""Dependency-free translation checks and safe, partial-locale scaffolding."""
import argparse
import json
from pathlib import Path
import re
import sys

from translation_checks import (
    CheckConfig,
    check as _check,
    content_coverage,
    flatten,
    locale_codes as _locale_codes,
    read_json,
    translation_errors,
    unique_object,
)

ROOT = Path(__file__).resolve().parents[1]
CATALOGS = {
    'chat': 'internal/domain/i18n/locales',
    'console': 'web/kit/lib/i18n/locales',
    'website': 'web/marketing/src/i18n/locales',
    'docs': 'web/docs/src/i18n/locales',
}
MANIFEST = 'internal/domain/i18n/locales.json'
LOCALE_RE = re.compile(r'[a-z]{2,3}(?:-[a-z0-9]{2,4})?')


def locale_codes(root):
    return _locale_codes(root, MANIFEST)


def check(root, strict=(), show_missing=False):
    return _check(CheckConfig(root, CATALOGS, MANIFEST, tuple(strict), show_missing, content_coverage))


def _validate_scaffold(code, name, og_locale):
    if not LOCALE_RE.fullmatch(code) or len(code) > 8:
        raise ValueError('Use a lowercase locale code such as es or pt-br (maximum 8 characters).')
    if not name.strip() or not re.fullmatch(r'[a-z]{2,3}_[A-Z]{2}', og_locale):
        raise ValueError('Supply the native language name and an Open Graph locale such as es_ES.')


def _scaffold_files(root, code, name, og_locale):
    contents = [{}, {'lang': {'name': name}}, {'lang.name': name, 'lang.ogLocale': og_locale}, {'lang.name': name}]
    paths = [root / directory / f'{code}.json' for directory in CATALOGS.values()]
    for path, content in zip(paths, contents):
        path.write_text(json.dumps(content, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    return paths


def scaffold(root, code, name, og_locale):
    _validate_scaffold(code, name, og_locale)
    codes = locale_codes(root)
    paths = [root / directory / f'{code}.json' for directory in CATALOGS.values()]
    if code in codes or any(path.exists() for path in paths):
        raise ValueError(f'{code} already exists; no files changed. Edit its existing catalogs instead.')
    _scaffold_files(root, code, name, og_locale)
    (root / MANIFEST).write_text(json.dumps(codes + [code], indent=2) + '\n', encoding='utf-8')
    print(f'Created partial {code} catalogs and registered the locale. Missing text falls back to English.')
    print('Translate values from the neighboring en.json files; do not copy untranslated English as completed work.')
    print('Website guide/legal content and documentation have separate files; see TRANSLATIONS.md.')


def _parser():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    audit = commands.add_parser('check', help='validate catalogs and report coverage')
    audit.add_argument('--strict', action='append', default=[], metavar='LOCALE', help='require every English key for this locale; repeatable')
    audit.add_argument('--missing', action='store_true', help='list missing keys to translate')
    new = commands.add_parser('new', help='create partial catalogs without overwriting existing work')
    new.add_argument('locale')
    new.add_argument('--name', required=True, help='native language name, e.g. Español')
    new.add_argument('--og-locale', required=True, help='social-card locale, e.g. es_ES')
    return parser


def main():
    args = _parser().parse_args()
    try:
        if args.command == 'check':
            return check(ROOT, args.strict, args.missing)
        scaffold(ROOT, args.locale, args.name, args.og_locale)
        return 0
    except ValueError as exc:
        print(f'ERROR {exc}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
