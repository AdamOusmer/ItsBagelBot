"""Small, dependency-free building blocks for the translation CLI."""
import json
import re
from dataclasses import dataclass

NAMED = re.compile(r'\{([A-Za-z_][A-Za-z_0-9.]*)\}')
PRINTF = re.compile(r'%(?:\[[0-9]+\])?[+#0 -]*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?[vTtbcdoOqxXUeEfFgGsp]')
LOCALE = re.compile(r'[a-z]{2,3}(?:-[a-z0-9]{2,4})?')


@dataclass(frozen=True)
class CheckConfig:
    root: object
    catalogs: dict
    manifest: str
    strict: tuple = ()
    show_missing: bool = False
    coverage: object = None
    codes: tuple = ()


@dataclass(frozen=True)
class LocaleConfig:
    config: CheckConfig
    folder: object
    directory: str
    source: dict
    code: str
    surface: str


def _string_list(value):
    return isinstance(value, str) or _is_string_list(value)


def _is_string_list(value):
    return isinstance(value, list) and all(isinstance(item, str) for item in value)


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


def _flatten_leaf(name, value):
    if _string_list(value):
        return {name: value}
    raise ValueError(f'{name}: expected a string, string array, or object')


def _flatten_entries(tree, prefix):
    result = {}
    for key, value in tree.items():
        name = f'{prefix}.{key}' if prefix else key
        leaves = flatten(value, name) if isinstance(value, dict) else _flatten_leaf(name, value)
        if result.keys() & leaves.keys():
            raise ValueError(f'{name}: ambiguous dotted key')
        result.update(leaves)
    return result


def flatten(tree, prefix=''):
    if not isinstance(tree, dict):
        raise ValueError(f'{prefix or "catalog"}: expected a JSON object')
    return _flatten_entries(tree, prefix)


def _placeholder_errors(source, target, printf):
    errors = []
    if source.strip() and not target.strip():
        errors.append('empty translation hides non-empty source text')
    if set(NAMED.findall(source)) != set(NAMED.findall(target)):
        errors.append('named placeholders differ from English')
    if printf and PRINTF.findall(source.replace('%%', '')) != PRINTF.findall(target.replace('%%', '')):
        errors.append('printf placeholders differ in type or order')
    return errors


def translation_errors(source, target, printf=False):
    if type(source) is not type(target):
        return ['translation changes the value type']
    if isinstance(source, list):
        if len(source) != len(target):
            return ['translation changes the array length']
        return [f'item {i}: {error}' for i, (a, b) in enumerate(zip(source, target))
                for error in translation_errors(a, b, printf)]
    return _placeholder_errors(source, target, printf)


def _valid_locale_list(codes):
    return isinstance(codes, list) and bool(codes) and all(
        isinstance(code, str) and LOCALE.fullmatch(code) and len(code) <= 8 for code in codes
    )


def locale_codes(root, manifest):
    codes = read_json(root / manifest)
    if not _valid_locale_list(codes):
        raise ValueError(f'{manifest}: expected lowercase locale codes (maximum 8 characters)')
    if len(codes) != len(set(codes)) or 'en' not in codes:
        raise ValueError(f'{manifest}: English is required and codes must be unique')
    return codes


def _catalog_errors(config, directory, surface):
    folder = config.root / directory
    source, source_error = _read_source(folder, directory)
    if source_error:
        return [source_error]
    found = {path.stem for path in folder.glob('*.json')}
    errors = _unregistered_errors(directory, found, config.codes)
    for code in sorted(set(config.codes) | found):
        item = LocaleConfig(config, folder, directory, source, code, surface)
        errors.extend(_locale_errors(item))
    return errors


def _read_source(folder, directory):
    try:
        source = flatten(read_json(folder / 'en.json'))
    except ValueError as exc:
        return None, str(exc)
    if not source:
        return None, f'{directory}/en.json: English catalog must not be empty'
    return source, None


def _unregistered_errors(directory, found, codes):
    return [f'{directory}/{code}.json: locale absent from manifest'
            for code in sorted(found - set(codes))]


def _locale_errors(item):
    errors = []
    path = item.folder / f'{item.code}.json'
    try:
        target = flatten(read_json(path)) if path.exists() else {}
    except ValueError as exc:
        return [str(exc)]
    errors.extend(_locale_shape_errors(item, target))
    errors.extend(_locale_value_errors(item, target))
    return errors


def _locale_shape_errors(item, target):
    missing = sorted(item.source.keys() - target.keys())
    extra = sorted(target.keys() - item.source.keys())
    matched = item.source.keys() & target.keys()
    print(f'{item.surface:8} {item.code:8} {len(matched)}/{len(item.source)} keys; {len(missing)} missing; {len(extra)} unknown')
    if item.config.show_missing:
        for key in missing:
            print(f'  MISSING {item.directory}/{item.code}.json: {key}')
    errors = []
    if missing and item.code in item.config.strict:
        errors.append(f'{item.directory}/{item.code}.json: {len(missing)} missing keys (complete locale required)')
    errors.extend(f'{item.directory}/{item.code}.json: unknown key {key}' for key in extra)
    return errors


def _locale_value_errors(item, target):
    matched = item.source.keys() & target.keys()
    return [f'{item.directory}/{item.code}.json: {key}: {error}'
            for key in sorted(matched)
            for error in translation_errors(item.source[key], target[key], item.surface == 'chat')]


def check(config):
    try:
        codes = locale_codes(config.root, config.manifest)
    except ValueError as exc:
        print(f'ERROR {exc}')
        return 1
    config = CheckConfig(config.root, config.catalogs, config.manifest, config.strict, config.show_missing, config.coverage, tuple(codes))
    errors = _unknown_strict_errors(config.strict, codes)
    errors.extend(_catalog_checks(config))
    errors.extend(_coverage_checks(config, codes))
    for error in errors:
        print(f'ERROR {error}')
    print('Translation checks failed.' if errors else 'Translation checks passed. Coverage counts entries/files, not translation quality or hardcoded UI text.')
    return int(bool(errors))


def _unknown_strict_errors(strict, codes):
    return [f'unknown strict locale: {code}' for code in strict if code not in codes]


def _catalog_checks(config):
    return [error for surface, directory in config.catalogs.items()
            for error in _catalog_errors(config, directory, surface)]


def _coverage_checks(config, codes):
    if config.coverage:
        return config.coverage(config.root, codes, config.strict, config.show_missing)
    return []


@dataclass(frozen=True)
class CoverageConfig:
    root: object
    docs: object
    codes: tuple
    strict: tuple
    show_missing: bool


def _doc_coverage(config):
    english = {path.relative_to(config.docs) for path in config.docs.rglob('*')
               if path.suffix in ('.md', '.mdx') and path.relative_to(config.docs).parts[0] not in config.codes}
    return [error for code in config.codes if code != 'en'
            for error in _doc_locale_errors(config, code, english)]


def _doc_locale_errors(config, code, english):
    missing = _missing_doc_paths(config, code, english)
    print(f'docs pages {code}: {len(english) - len(missing)}/{len(english)} translated files')
    if config.show_missing:
        for path in missing:
            print(f'  MISSING {config.docs.relative_to(config.root)}/{code}/{path}')
    if missing and code in config.strict:
        return [f'docs {code}: {len(missing)} untranslated pages (missing or identical to English)']
    return []


def _missing_doc_paths(config, code, english):
    return sorted(str(path) for path in english
                  if not (config.docs / code / path).exists()
                  or (config.docs / code / path).read_bytes() == (config.docs / path).read_bytes())


def _legal_coverage(config, legal):
    return [error for document in sorted(path for path in legal.iterdir() if path.is_dir())
            for error in _legal_document_errors(config, document)]


def _legal_document_errors(config, document):
    english = [path.name for path in (document / 'en').iterdir() if path.is_file()]
    return [error for code in config.codes if code != 'en'
            for error in _legal_locale_errors(config, document, english, code)]


def _legal_locale_errors(config, document, english, code):
    missing = [name for name in english if not (document / code / name).exists()]
    print(f'legal {document.name} {code}: {len(english) - len(missing)}/{len(english)} files')
    if missing and code in config.strict:
        return [f'legal {document.name} {code}: missing {", ".join(missing)}']
    return []


def content_coverage(root, codes, strict=(), show_missing=False):
    config = CoverageConfig(root, root / 'web/docs/src/content/docs', tuple(codes), tuple(strict), show_missing)
    errors = []
    if config.docs.exists():
        errors.extend(_doc_coverage(config))
    legal = root / 'web/marketing/src/content/legal'
    if legal.exists():
        errors.extend(_legal_coverage(config, legal))
    return errors
