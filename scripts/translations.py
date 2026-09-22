#!/usr/bin/env python3
"""Dependency-free translation checks and safe, partial-locale scaffolding."""
import argparse
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
CATALOGS = {
    'chat': 'internal/domain/i18n/locales',
    'console': 'web/kit/lib/i18n/locales',
    'website': 'web/marketing/src/i18n/locales',
    'docs': 'web/docs/src/i18n/locales',
}
MANIFEST = 'internal/domain/i18n/locales.json'
NAMED = re.compile(r'\{([A-Za-z_][A-Za-z_0-9.]*)\}')
PRINTF = re.compile(r'%(?:\[[0-9]+\])?[+#0 -]*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?[vTtbcdoOqxXUeEfFgGsp]')


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f'duplicate key {key!r}')
        result[key] = value
    return result


def read_json(path):
    try:
        return json.loads(path.read_text(encoding='utf-8'), object_pairs_hook=unique_object)
    except (OSError, ValueError) as exc:
        raise ValueError(f'{path}: {exc}') from exc


def flatten(tree, prefix=''):
    if not isinstance(tree, dict):
        raise ValueError(f'{prefix or "catalog"}: expected a JSON object')
    result = {}
    for key, value in tree.items():
        name = f'{prefix}.{key}' if prefix else key
        if isinstance(value, dict):
            leaves = flatten(value, name)
        elif isinstance(value, str) or (isinstance(value, list) and all(isinstance(x, str) for x in value)):
            leaves = {name: value}
        else:
            raise ValueError(f'{name}: expected a string, string array, or object')
        if result.keys() & leaves.keys():
            raise ValueError(f'{name}: ambiguous dotted key')
        result.update(leaves)
    return result


def translation_errors(source, target, printf=False):
    if type(source) is not type(target):
        return ['translation changes the value type']
    if isinstance(source, list):
        if len(source) != len(target):
            return ['translation changes the array length']
        return [f'item {i}: {err}' for i, (a, b) in enumerate(zip(source, target))
                for err in translation_errors(a, b, printf)]
    errors = []
    if source.strip() and not target.strip():
        errors.append('empty translation hides non-empty source text')
    if set(NAMED.findall(source)) != set(NAMED.findall(target)):
        errors.append('named placeholders differ from English')
    if printf and PRINTF.findall(source.replace('%%', '')) != PRINTF.findall(target.replace('%%', '')):
        errors.append('printf placeholders differ in type or order')
    return errors


def locale_codes(root):
    codes = read_json(root / MANIFEST)
    if not isinstance(codes, list) or not codes or any(not isinstance(c, str) or not re.fullmatch(r'[a-z]{2,3}(?:-[a-z0-9]{2,4})?', c) or len(c) > 8 for c in codes):
        raise ValueError(f'{MANIFEST}: expected lowercase locale codes (maximum 8 characters)')
    if len(codes) != len(set(codes)) or 'en' not in codes:
        raise ValueError(f'{MANIFEST}: English is required and codes must be unique')
    return codes


def check(root, strict=(), show_missing=False):
    errors = []
    try:
        codes = locale_codes(root)
    except ValueError as exc:
        print(f'ERROR {exc}')
        return 1
    for code in strict:
        if code not in codes:
            errors.append(f'unknown strict locale: {code}')
    for surface, directory in CATALOGS.items():
        folder = root / directory
        try:
            source = flatten(read_json(folder / 'en.json'))
            if not source:
                raise ValueError(f'{directory}/en.json: English catalog must not be empty')
        except ValueError as exc:
            errors.append(str(exc))
            continue
        found = {p.stem for p in folder.glob('*.json')}
        for code in sorted(found - set(codes)):
            errors.append(f'{directory}/{code}.json: locale absent from {MANIFEST}')
        for code in sorted(set(codes) | found):
            path = folder / f'{code}.json'
            try:
                target = flatten(read_json(path)) if path.exists() else {}
            except ValueError as exc:
                errors.append(str(exc))
                continue
            missing = sorted(source.keys() - target.keys())
            extra = sorted(target.keys() - source.keys())
            matched = source.keys() & target.keys()
            print(f'{surface:8} {code:8} {len(matched)}/{len(source)} keys; {len(missing)} missing; {len(extra)} unknown')
            if show_missing:
                for key in missing:
                    print(f'  MISSING {directory}/{code}.json: {key}')
            if missing and code in strict:
                errors.append(f'{directory}/{code}.json: {len(missing)} missing keys (complete locale required)')
            for key in extra:
                errors.append(f'{directory}/{code}.json: unknown key {key}')
            for key in sorted(matched):
                for error in translation_errors(source[key], target[key], surface == 'chat'):
                    errors.append(f'{directory}/{code}.json: {key}: {error}')
    errors.extend(content_coverage(root, codes, strict, show_missing))
    for error in errors:
        print(f'ERROR {error}')
    print('Translation checks failed.' if errors else 'Translation checks passed. Coverage counts entries/files, not translation quality or hardcoded UI text.')
    return int(bool(errors))


def content_coverage(root, codes, strict=(), show_missing=False):
    """File-level coverage; builds and human review validate rendered content."""
    errors = []
    docs = root / 'web/docs/src/content/docs'
    if docs.exists():
        english = {p.relative_to(docs) for p in docs.rglob('*')
                   if p.suffix in ('.md', '.mdx') and p.relative_to(docs).parts[0] not in codes}
        for code in codes:
            if code == 'en':
                continue
            missing = sorted(str(path) for path in english
                             if not (docs / code / path).exists()
                             or (docs / code / path).read_bytes() == (docs / path).read_bytes())
            print(f'docs pages {code}: {len(english) - len(missing)}/{len(english)} translated files')
            if show_missing:
                for path in missing:
                    print(f'  MISSING {docs.relative_to(root)}/{code}/{path}')
            if missing and code in strict:
                errors.append(f'docs {code}: {len(missing)} untranslated pages (missing or identical to English)')
    legal = root / 'web/marketing/src/content/legal'
    if legal.exists():
        for document in sorted(p for p in legal.iterdir() if p.is_dir()):
            english = [p.name for p in (document / 'en').iterdir() if p.is_file()]
            for code in codes:
                if code == 'en':
                    continue
                missing = [name for name in english if not (document / code / name).exists()]
                print(f'legal {document.name} {code}: {len(english) - len(missing)}/{len(english)} files')
                if missing and code in strict:
                    errors.append(f'legal {document.name} {code}: missing {", ".join(missing)}')
    return errors


def scaffold(root, code, name, og_locale):
    if not re.fullmatch(r'[a-z]{2,3}(?:-[a-z0-9]{2,4})?', code) or len(code) > 8:
        raise ValueError('Use a lowercase locale code such as es or pt-br (maximum 8 characters).')
    if not name.strip() or not re.fullmatch(r'[a-z]{2,3}_[A-Z]{2}', og_locale):
        raise ValueError('Supply the native language name and an Open Graph locale such as es_ES.')
    codes = locale_codes(root)
    files = [root / directory / f'{code}.json' for directory in CATALOGS.values()]
    if code in codes or any(path.exists() for path in files):
        raise ValueError(f'{code} already exists; no files changed. Edit its existing catalogs instead.')
    contents = [{}, {'lang': {'name': name}}, {'lang.name': name, 'lang.ogLocale': og_locale}, {'lang.name': name}]
    for path, content in zip(files, contents):
        path.write_text(json.dumps(content, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    (root / MANIFEST).write_text(json.dumps(codes + [code], indent=2) + '\n', encoding='utf-8')
    print(f'Created partial {code} catalogs and registered the locale. Missing text falls back to English.')
    print('Translate values from the neighboring en.json files; do not copy untranslated English as completed work.')
    print('Website guide/legal content and documentation have separate files; see TRANSLATIONS.md.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    audit = commands.add_parser('check', help='validate catalogs and report coverage')
    audit.add_argument('--strict', action='append', default=[], metavar='LOCALE', help='require every English key for this locale; repeatable')
    audit.add_argument('--missing', action='store_true', help='list missing keys to translate')
    new = commands.add_parser('new', help='create partial catalogs without overwriting existing work')
    new.add_argument('locale')
    new.add_argument('--name', required=True, help='native language name, e.g. Español')
    new.add_argument('--og-locale', required=True, help='social-card locale, e.g. es_ES')
    args = parser.parse_args()
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
