"""Small, dependency-free building blocks for the translation CLI."""
import json
import re
from dataclasses import dataclass, field
from pathlib import PurePosixPath

NAMED = re.compile(r'\{([A-Za-z_][A-Za-z_0-9.]*)\}')
PRINTF = re.compile(r'%(?:\[[0-9]+\])?[+#0 -]*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?[vTtbcdoOqxXUeEfFgGsp]')
LOCALE = re.compile(r'[a-z]{2,3}(?:-[a-z0-9]{2,4})?')
NAME = re.compile(r'[a-z][a-zA-Z0-9_]*')
NAME_RULE = 'names start with a lowercase letter and use only letters, digits and _, like timers or channel_points'
TREE = 'locales'
MANIFEST = f'{TREE}/manifest.json'
ROOT_FILES = frozenset({'manifest.json', 'embed.go'})
IGNORED = frozenset({'.DS_Store'})
MAX_LEAVES = 150
ARRAY_SURFACES = frozenset({'console'})
PRINTF_SURFACES = frozenset({'chat'})
FLAT_SURFACES = frozenset({'chat'})
NESTED_SURFACES = frozenset({'console'})
EXTERNAL_SURFACES = {'ui': 'ui/locales'}
EXTERNAL_INDEX = 'index.ts'


@dataclass(frozen=True)
class CheckConfig:
    root: object
    strict: tuple = ()
    show_missing: bool = False
    coverage: object = None
    strict_catalogs: tuple = ()


@dataclass
class Catalog:
    label: str
    keys: dict = field(default_factory=dict)
    files: list = field(default_factory=list)
    errors: list = field(default_factory=list)

    def path(self, relative):
        return f'{self.label}/{relative}'


@dataclass
class Comparison:
    matched: set = field(default_factory=set)
    unknown: int = 0
    errors: list = field(default_factory=list)


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


def key_prefix(relative):
    parts = list(PurePosixPath(relative).with_suffix('').parts)
    if parts[-1] == 'index':
        parts.pop()
    return '.'.join(parts)


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


def locale_codes(root, manifest=MANIFEST):
    codes = read_json(root / manifest)
    if not _valid_locale_list(codes):
        raise ValueError(f'{manifest}: expected lowercase locale codes (maximum 8 characters)')
    if len(codes) != len(set(codes)) or 'en' not in codes:
        raise ValueError(f'{manifest}: English is required and codes must be unique')
    return codes


def english_surfaces(root):
    folder = root / TREE / 'en'
    if not folder.is_dir():
        raise ValueError(f'{TREE}/en: the English reference folder is missing')
    surfaces = sorted(path.name for path in folder.iterdir() if path.is_dir() and NAME.fullmatch(path.name))
    if not surfaces:
        raise ValueError(f'{TREE}/en: add at least one surface folder, such as chat or console')
    return surfaces + [surface for surface, base in EXTERNAL_SURFACES.items() if (root / base / 'en').is_dir()]


def surface_label(code, surface):
    base = EXTERNAL_SURFACES.get(surface)
    return f'{base}/{code}' if base else f'{TREE}/{code}/{surface}'


def read_catalog(root, code, surface):
    catalog = Catalog(surface_label(code, surface))
    folder = root / catalog.label
    if folder.is_dir():
        for relative in _catalog_files(folder, catalog, surface):
            _add_file(catalog, folder / relative, relative, surface)
    if surface in NESTED_SURFACES:
        catalog.errors.extend(_branch_errors(catalog))
    return catalog


def _branch_errors(catalog):
    return [f'{catalog.path(relative)}: key {key} needs {parent} to be a group, but '
            f'{catalog.path(catalog.keys[parent][1])} defines {parent} as text; rename one of them'
            for key, (_, relative) in catalog.keys.items()
            for parent in _parent_keys(key) if parent in catalog.keys]


def _parent_keys(key):
    parts = key.split('.')
    return ['.'.join(parts[:end]) for end in range(1, len(parts))]


def _catalog_files(folder, catalog, surface):
    files = []
    for path in sorted(folder.rglob('*')):
        if path.name in IGNORED:
            continue
        relative = path.relative_to(folder).as_posix()
        error = _entry_error(path, surface)
        if error:
            catalog.errors.append(f'{catalog.path(relative)}: {error}')
        elif path.is_file() and _readable_parents(relative, surface):
            files.append(relative)
    return files


def _entry_error(path, surface):
    if path.is_dir():
        return _folder_error(path.name, surface)
    if path.suffix != '.json':
        return 'only .json files belong in the locales tree'
    return None if NAME.fullmatch(path.stem) else f'rename the file; {NAME_RULE}'


def _folder_error(name, surface):
    if surface in FLAT_SURFACES:
        return f'{surface} files cannot live in subfolders; keep them directly in {surface}/ and use dotted keys'
    return None if NAME.fullmatch(name) else f'rename the folder; {NAME_RULE}'


def _readable_parents(relative, surface):
    parents = PurePosixPath(relative).parent.parts
    if surface in FLAT_SURFACES:
        return not parents
    return all(NAME.fullmatch(part) for part in parents)


def _add_file(catalog, path, relative, surface):
    name = catalog.path(relative)
    catalog.files.append(relative)
    try:
        tree = json.loads(path.read_text(encoding='utf-8'), object_pairs_hook=unique_object)
        leaves = flatten(tree, key_prefix(relative))
    except (OSError, ValueError) as exc:
        catalog.errors.append(f'{name}: {exc}')
        return
    catalog.errors.extend(f'{name}: {error}' for error in _file_errors(tree, leaves, surface))
    _merge(catalog, leaves, relative)


def _file_errors(tree, leaves, surface):
    errors = []
    if len(leaves) > MAX_LEAVES:
        errors.append(f'{len(leaves)} keys, over the limit of {MAX_LEAVES} per file; '
                      'split it into a folder with index.json plus one file per group')
    if surface not in ARRAY_SURFACES:
        errors.extend(f'{key}: use a string; lists are only allowed in console files'
                      for key, value in leaves.items() if isinstance(value, list))
    if surface in FLAT_SURFACES:
        errors.extend(f'{key}: {surface} files hold flat "key": "text" pairs; write nested keys with dots'
                      for key, value in tree.items() if isinstance(value, dict))
    return errors


def _merge(catalog, leaves, relative):
    for key, value in leaves.items():
        owner = catalog.keys.get(key)
        if owner:
            catalog.errors.append(f'{catalog.path(relative)}: key {key} is also defined in '
                                  f'{catalog.path(owner[1])}; keep each key in one file')
        else:
            catalog.keys[key] = (value, relative)


def compare(english, target, surface):
    result = Comparison()
    orphans = set(target.files) - set(english.files)
    result.errors.extend(f'{target.path(relative)}: no English file at {english.path(relative)}; '
                         'use the same file paths as the English folder' for relative in sorted(orphans))
    printf = surface in PRINTF_SURFACES
    for key, (value, relative) in target.keys.items():
        placed, errors = _placement(english, target, (key, relative), orphans)
        result.errors.extend(errors)
        if not placed:
            result.unknown += 1
            continue
        result.matched.add(key)
        result.errors.extend(f'{target.path(relative)}: {key}: {problem}'
                             for problem in translation_errors(english.keys[key][0], value, printf))
    return result


def _placement(english, target, entry, orphans):
    key, relative = entry
    if relative in orphans:
        return False, []
    owner = english.keys.get(key)
    if owner is None:
        return False, [f'{target.path(relative)}: unknown key {key}; {english.path(relative)} has no such key']
    if owner[1] != relative:
        return False, [f'{target.path(relative)}: {key} belongs in {target.path(owner[1])}, '
                       f'matching {english.path(owner[1])}']
    return True, []


def missing_by_file(english, matched):
    groups = {}
    for key, (_, relative) in english.keys.items():
        if key not in matched:
            groups.setdefault(relative, []).append(key)
    return groups


def layout_errors(root, codes, surfaces):
    tree_surfaces = [surface for surface in surfaces if surface not in EXTERNAL_SURFACES]
    errors = [error for path in sorted((root / TREE).iterdir())
              for error in _top_level_errors(path, codes, tree_surfaces)]
    for surface in surfaces:
        if surface in EXTERNAL_SURFACES:
            errors.extend(_external_layout_errors(root, EXTERNAL_SURFACES[surface], codes))
    return errors


def _skips_external_entry(path):
    return path.name in IGNORED or (path.is_file() and path.name == EXTERNAL_INDEX)


def _external_layout_errors(root, base, codes):
    errors = []
    for path in sorted((root / base).iterdir()):
        if _skips_external_entry(path):
            continue
        if not path.is_dir():
            errors.append(f'{base}/{path.name}: only locale folders and {EXTERNAL_INDEX} belong directly in {base}/')
        elif path.name not in codes:
            errors.append(f'{base}/{path.name}: locale absent from manifest; register it in {MANIFEST} or remove the folder')
    return errors + _external_index_errors(root, base)


def _external_index_errors(root, base):
    index = root / base / EXTERNAL_INDEX
    listed = index.read_text(encoding='utf-8') if index.is_file() else ''
    return [f'{base}/{path.name}: not loaded yet; run `bun ui/scripts/gen-locales.mjs` and commit {base}/{EXTERNAL_INDEX}'
            for path in sorted((root / base).iterdir()) if path.is_dir() and f"'{path.name}': {{" not in listed]


def _top_level_errors(path, codes, surfaces):
    label = f'{TREE}/{path.name}'
    if _expected_top_level_file(path):
        return []
    if not path.is_dir():
        return [f'{label}: only locale folders, manifest.json and embed.go belong directly in {TREE}/']
    if path.name not in codes:
        return [f'{label}: locale absent from manifest; register it in {MANIFEST} or remove the folder']
    return [f'{label}/{entry.name}: {_locale_entry_error(entry, surfaces)}'
            for entry in sorted(path.iterdir()) if not _expected_locale_entry(entry, surfaces)]


def _expected_top_level_file(path):
    return path.name in IGNORED or (path.is_file() and path.name in ROOT_FILES)


def _expected_locale_entry(entry, surfaces):
    return entry.name in IGNORED or (entry.is_dir() and entry.name in surfaces)


def _locale_entry_error(entry, surfaces):
    if entry.is_file():
        return f'files belong inside a surface folder ({", ".join(surfaces)})'
    if entry.parent.name == 'en':
        return f'rename the surface folder; {NAME_RULE}'
    return f'no English counterpart at {TREE}/en/{entry.name}; surfaces are {", ".join(surfaces)}'


def check(config):
    try:
        codes = locale_codes(config.root)
        surfaces = english_surfaces(config.root)
    except ValueError as exc:
        print(f'ERROR {exc}')
        return 1
    errors = _unknown_strict_errors(sorted(set(config.strict) | set(config.strict_catalogs)), codes)
    errors.extend(layout_errors(config.root, codes, surfaces))
    for surface in surfaces:
        errors.extend(_surface_checks(config, codes, surface))
    errors.extend(_coverage_checks(config, codes))
    for error in errors:
        print(f'ERROR {error}')
    print('Translation checks failed.' if errors else 'Translation checks passed. Coverage counts entries/files, not translation quality or hardcoded UI text.')
    return int(bool(errors))


def _unknown_strict_errors(strict, codes):
    return [f'unknown strict locale: {code}' for code in strict if code not in codes]


def _surface_checks(config, codes, surface):
    english = read_catalog(config.root, 'en', surface)
    errors = list(english.errors)
    if not english.keys:
        return errors + [f'{english.label}: English catalog must not be empty']
    print(f'{surface:8} {"en":8} {len(english.keys)} keys in {_plural(len(english.files), "file")} (reference)')
    for code in codes:
        if code != 'en':
            errors.extend(_locale_checks(config, english, code, surface))
    return errors


def _locale_checks(config, english, code, surface):
    target = read_catalog(config.root, code, surface)
    result = compare(english, target, surface)
    missing = missing_by_file(english, result.matched)
    count = sum(len(keys) for keys in missing.values())
    print(f'{surface:8} {code:8} {len(result.matched)}/{len(english.keys)} keys; {count} missing; {result.unknown} unknown')
    if config.show_missing:
        _print_missing(target, missing)
    errors = target.errors + result.errors
    if code in config.strict or code in config.strict_catalogs:
        errors.extend(f'{target.path(relative)}: {len(keys)} missing (complete locale required)'
                      for relative, keys in missing.items())
    return errors


def _plural(count, noun):
    return f'{count} {noun}' if count == 1 else f'{count} {noun}s'


def _print_missing(target, missing):
    for relative, keys in missing.items():
        for key in keys:
            print(f'  MISSING {target.path(relative)}: {key}')


def _coverage_checks(config, codes):
    if config.coverage:
        return config.coverage(config.root, codes, config.strict, config.show_missing)
    return []


def status(root, code, show_keys=False):
    if code not in locale_codes(root):
        raise ValueError(f'{code} is not registered in {MANIFEST}')
    problems = [problem for surface in english_surfaces(root)
                for problem in _surface_status(root, code, surface, show_keys)]
    for problem in problems:
        print(f'ERROR {problem}')
    return int(bool(problems))


def _surface_status(root, code, surface, show_keys):
    english = read_catalog(root, 'en', surface)
    target = read_catalog(root, code, surface) if code != 'en' else english
    result = compare(english, target, surface)
    missing = missing_by_file(english, result.matched)
    print(f'{surface}: {len(result.matched)}/{len(english.keys)} keys translated')
    for relative, keys in missing.items():
        note = '' if relative in target.files else ' (new file)'
        print(f'  {target.path(relative)}: {len(keys)} missing{note}')
        for key in keys if show_keys else ():
            print(f'    {key}')
    return target.errors + result.errors


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
