"""Regression checks for translation mistakes that can break rendered messages."""
import contextlib
from collections import Counter
from html.parser import HTMLParser
import re
import io
import json
from pathlib import Path
import tempfile
import unittest

import translations as t

ENGLISH = {
    'chat/index.json': {'ping': 'Pong after %s'},
    'chat/cmd.json': {'added': '@{user} added {command}', 'err.missing': 'Missing %d of %s'},
    'console/commands.json': {'title': 'Commands', 'row': {'edit': 'Edit {name}'}, 'tips': ['One', 'Two']},
    'console/admin/index.json': {'title': 'Admin'},
    'console/admin/users.json': {'pagerNext': 'Next {page}'},
    'console/lang.json': {'label': 'Language', 'name': 'English'},
    'website/lang.json': {'switch': 'Switch', 'name': 'English', 'ogLocale': 'en_US'},
    'website/hero.json': {'title': 'Hello'},
    'docs/index.json': {'title': 'Docs', 'lang.name': 'English'},
}

FRENCH = {
    'chat/index.json': {'ping': 'Pong après %s'},
    'chat/cmd.json': {'added': '@{user} a ajouté {command}', 'err.missing': 'Il manque %d sur %s'},
    'console/commands.json': {'title': 'Commandes', 'row.edit': 'Modifier {name}', 'tips': ['Un', 'Deux']},
    'console/admin/index.json': {'title': 'Administration'},
    'console/admin/users.json': {'pagerNext': 'Suivant {page}'},
    'console/lang.json': {'label': 'Langue', 'name': 'Français'},
    'website/lang.json': {'switch': 'Changer', 'name': 'Français', 'ogLocale': 'fr_FR'},
    'website/hero.json': {'title': 'Bonjour'},
    'docs/index.json': {'title': 'Documentation', 'lang.name': 'Français'},
}


def write(root, relative, content):
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    text = content if isinstance(content, str) else json.dumps(content, ensure_ascii=False)
    path.write_text(text, encoding='utf-8')
    return path


def tree(root, french=FRENCH):
    for relative, content in ENGLISH.items():
        write(root, f'locales/en/{relative}', content)
    for relative, content in french.items():
        write(root, f'locales/fr/{relative}', content)
    write(root, t.MANIFEST, '["en", "fr"]')
    write(root, 'locales/embed.go', 'package locales\n')


def run(function, *args):
    output = io.StringIO()
    with contextlib.redirect_stdout(output):
        code = function(*args)
    return code, output.getvalue()


class Fixture(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.root = Path(self.folder.name)
        tree(self.root)

    def tearDown(self):
        self.folder.cleanup()

    def assertOutput(self, output, *messages):
        for message in messages:
            self.assertIn(message, output)

    def assertFails(self, *messages, strict=()):
        code, output = run(t.check, self.root, strict)
        self.assertEqual(code, 1, output)
        self.assertOutput(output, *messages)
        return output


class TranslationChecks(unittest.TestCase):
    def test_duplicate_keys_are_rejected(self):
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            json.loads('{"hello":"a","hello":"b"}', object_pairs_hook=t.unique_object)

    def test_named_placeholders_can_move_but_cannot_disappear(self):
        self.assertEqual(t.translation_errors('{user}: {count}', '{count} pour {user}'), [])
        self.assertTrue(t.translation_errors('{user}: {count}', '{count} pour {utilisateur}'))

    def test_printf_order_and_escaped_percent(self):
        self.assertEqual(t.translation_errors('%s: %d (100%%)', '%s : %d (100%%)', True), [])
        self.assertTrue(t.translation_errors('%s: %d', '%d : %s', True))
        self.assertEqual(t.translation_errors('Save 10% today', 'Économisez 10 %', False), [])

    def test_arrays_and_empty_values(self):
        self.assertTrue(t.translation_errors(['One', 'Two'], ['Un']))
        self.assertTrue(t.translation_errors('Hello', '  '))
        self.assertEqual(t.translation_errors('', ''), [])
        self.assertTrue(t.translation_errors('Hello', ['Bonjour']))

    def test_flatten_and_invalid_leaves(self):
        self.assertEqual(t.flatten({'a': {'b': 'B'}}), {'a.b': 'B'})
        with self.assertRaises(ValueError):
            t.flatten({'a': 42})
        with self.assertRaises(ValueError):
            t.flatten({'a.b': 'B', 'a': {'b': 'Other'}})

    def test_key_prefix_follows_the_file_path(self):
        expected = {'commands.json': 'commands', 'admin/index.json': 'admin', 'admin/users.json': 'admin.users', 'index.json': ''}
        self.assertEqual({path: t.key_prefix(path) for path in expected}, expected)

    def test_docs_coverage_rejects_english_staging_copies(self):
        with tempfile.TemporaryDirectory() as folder, contextlib.redirect_stdout(io.StringIO()):
            root = Path(folder)
            docs = root / 'web/docs/src/content/docs'
            (docs / 'fr').mkdir(parents=True)
            (docs / 'index.md').write_text('English source')
            (docs / 'fr/index.md').write_text('English source')
            self.assertTrue(t.content_coverage(root, ['en', 'fr'], ['fr']))
            (docs / 'fr/index.md').write_text('Traduction française')
            self.assertEqual(t.content_coverage(root, ['en', 'fr'], ['fr']), [])

    def test_demo_checkout_catalog_preserves_keys_and_placeholders(self):
        folder = (t.ROOT / 'web/dashboard/src/routes/(app)/billing/demo-checkout/copy')
        english = json.loads((folder / 'en.json').read_text(encoding='utf-8'))
        french = json.loads((folder / 'fr.json').read_text(encoding='utf-8'))
        self.assertEqual(set(english), set(french))
        for key, value in english.items():
            with self.subTest(key=key):
                self.assertEqual(t.translation_errors(value, french[key]), [])


class TreeLayout(Fixture):
    def test_keys_derive_from_paths_index_files_nesting_and_dots(self):
        catalogs = {surface: t.read_catalog(self.root, 'en', surface) for surface in ('console', 'chat', 'docs')}
        self.assertEqual({surface: list(catalog.keys) for surface, catalog in catalogs.items()}, {
            'console': ['admin.title', 'admin.users.pagerNext', 'commands.title', 'commands.row.edit', 'commands.tips',
                        'lang.label', 'lang.name'],
            'chat': ['cmd.added', 'cmd.err.missing', 'ping'],
            'docs': ['title', 'lang.name'],
        })
        self.assertEqual(catalogs['console'].keys['commands.tips'], (['One', 'Two'], 'commands.json'))

    def test_complete_tree_passes_strict_and_reports_coverage(self):
        code, output = run(t.check, self.root, ['fr'])
        self.assertEqual(code, 0, output)
        self.assertOutput(output, 'console  fr       7/7 keys; 0 missing; 0 unknown',
                          'console  en       7 keys in 4 files (reference)', 'docs     en       2 keys in 1 file (reference)')

    def test_dotted_keys_match_nested_english_keys(self):
        french = t.read_catalog(self.root, 'fr', 'console')
        english = t.read_catalog(self.root, 'en', 'console')
        self.assertIn('commands.row.edit', t.compare(english, french, 'console').matched)

    def test_partial_locale_warns_but_strict_locale_fails(self):
        (self.root / 'locales/fr/console/admin/users.json').unlink()
        code, output = run(t.check, self.root, (), True)
        self.assertEqual(code, 0, output)
        self.assertOutput(output, 'console  fr       6/7 keys; 1 missing; 0 unknown',
                          'MISSING locales/fr/console/admin/users.json: admin.users.pagerNext')
        self.assertFails('locales/fr/console/admin/users.json: 1 missing (complete locale required)', strict=['fr'])

    def test_registered_locale_without_a_folder_is_partial(self):
        write(self.root, t.MANIFEST, '["en", "fr", "es"]')
        self.assertEqual(run(t.check, self.root)[0], 0)
        self.assertFails('locales/es/chat/cmd.json: 2 missing (complete locale required)', strict=['es'])

    def test_unregistered_locale_fails(self):
        write(self.root, 'locales/es/chat/cmd.json', {})
        self.assertFails('locales/es: locale absent from manifest')

    def test_unknown_strict_locale_fails(self):
        self.assertFails('unknown strict locale: de', strict=['de'])

    def test_manifest_and_embed_are_the_only_top_level_files(self):
        write(self.root, 'locales/.DS_Store', '')
        self.assertEqual(run(t.check, self.root)[0], 0)
        write(self.root, 'locales/README.md', 'notes')
        self.assertFails('locales/README.md: only locale folders, manifest.json and embed.go')

    def test_locale_folders_hold_only_english_surfaces(self):
        write(self.root, 'locales/fr/stray.json', {})
        write(self.root, 'locales/fr/emails/welcome.json', {})
        self.assertFails('locales/fr/stray.json: files belong inside a surface folder',
                         'locales/fr/emails: no English counterpart at locales/en/emails')

    def test_only_json_files_with_simple_names(self):
        write(self.root, 'locales/fr/console/.DS_Store', '')
        self.assertEqual(run(t.check, self.root)[0], 0)
        write(self.root, 'locales/fr/console/notes.txt', 'draft')
        write(self.root, 'locales/en/console/Bad-Name.json', {})
        write(self.root, 'locales/en/console/Admin Tools/list.json', {'title': 'Tools'})
        self.assertFails('locales/fr/console/notes.txt: only .json files belong in the locales tree',
                         'locales/en/console/Bad-Name.json: rename the file',
                         'locales/en/console/Admin Tools: rename the folder')


class TreeRules(Fixture):
    def test_file_over_the_leaf_cap_fails_and_arrays_count_once(self):
        english = {f'k{i}': 'Text' for i in range(t.MAX_LEAVES - 1)}
        english['list'] = ['a', 'b', 'c']
        write(self.root, 'locales/en/console/big.json', english)
        self.assertEqual(run(t.check, self.root)[0], 0)
        english['extra'] = 'One too many'
        write(self.root, 'locales/en/console/big.json', english)
        self.assertFails(f'locales/en/console/big.json: {t.MAX_LEAVES + 1} keys, over the limit of {t.MAX_LEAVES}')

    def test_cap_applies_to_non_english_files(self):
        write(self.root, 'locales/fr/website/hero.json', {f'k{i}': 'Texte' for i in range(t.MAX_LEAVES + 1)})
        self.assertFails(f'locales/fr/website/hero.json: {t.MAX_LEAVES + 1} keys, over the limit')

    def test_non_english_file_must_mirror_an_english_path(self):
        write(self.root, 'locales/fr/console/timers.json', {'title': 'Minuteurs'})
        output = self.assertFails('locales/fr/console/timers.json: no English file at locales/en/console/timers.json')
        self.assertIn('0 missing; 1 unknown', output)

    def test_unknown_key_names_file_and_key(self):
        write(self.root, 'locales/fr/console/admin/users.json', {'pagerNext': 'Suivant {page}', 'pagerPrev': 'Précédent'})
        self.assertFails('locales/fr/console/admin/users.json: unknown key admin.users.pagerPrev')

    def test_key_in_the_wrong_file_points_to_the_right_one(self):
        (self.root / 'locales/fr/console/admin/users.json').unlink()
        write(self.root, 'locales/fr/console/admin/index.json', {'title': 'Administration', 'users.pagerNext': 'Suivant {page}'})
        self.assertFails('locales/fr/console/admin/index.json: admin.users.pagerNext belongs in '
                         'locales/fr/console/admin/users.json')

    def test_one_full_key_is_defined_by_one_file(self):
        write(self.root, 'locales/en/console/admin.json', {'title': 'Other admin'})
        output = self.assertFails('key admin.title is also defined in')
        line = next(line for line in output.splitlines() if 'key admin.title is also defined in' in line)
        self.assertOutput(line, 'locales/en/console/admin.json', 'locales/en/console/admin/index.json')

    def test_console_key_cannot_be_both_text_and_a_group(self):
        write(self.root, 'locales/en/console/admin/index.json', {'title': 'Admin', 'users': 'Users'})
        self.assertFails('locales/en/console/admin/users.json: key admin.users.pagerNext needs admin.users to be a group, '
                         'but locales/en/console/admin/index.json defines admin.users as text')
        write(self.root, 'locales/en/console/admin/index.json', {'title': 'Admin'})
        write(self.root, 'locales/fr/console/commands.json', {'title': 'Commandes', 'row': 'Ligne', 'row.edit': 'Modifier {name}',
                                                              'tips': ['Un', 'Deux']})
        self.assertFails('locales/fr/console/commands.json: key commands.row.edit needs commands.row to be a group')

    def test_flat_surfaces_allow_a_key_beside_its_dotted_children(self):
        write(self.root, 'locales/en/chat/index.json', {'ping': 'Pong after %s', 'uptime': 'Live for %s'})
        write(self.root, 'locales/en/chat/uptime.json', {'offline': 'Offline', 'unavailable': 'Unavailable'})
        write(self.root, 'locales/en/website/hero.json', {'title': 'Hello', 'title.sub': 'World'})
        code, output = run(t.check, self.root)
        self.assertEqual(code, 0, output)

    def test_dotted_and_nested_duplicates_inside_a_file_fail(self):
        write(self.root, 'locales/fr/console/commands.json', {'title': 'Commandes', 'row.edit': 'A {name}',
                                                              'row': {'edit': 'B {name}'}, 'tips': ['Un', 'Deux']})
        self.assertFails('locales/fr/console/commands.json: commands.row: ambiguous dotted key')

    def test_invalid_json_duplicate_keys_and_values_name_the_file(self):
        write(self.root, 'locales/fr/website/hero.json', '{"title": "Bonjour",}')
        write(self.root, 'locales/fr/website/lang.json', '{"name": "A", "name": "B"}')
        write(self.root, 'locales/fr/docs/index.json', {'title': 42})
        self.assertFails('locales/fr/website/hero.json: Expecting property name',
                         "locales/fr/website/lang.json: duplicate key 'name'",
                         'locales/fr/docs/index.json: title: expected a string')

    def test_placeholder_damage_and_empty_values_fail(self):
        write(self.root, 'locales/fr/console/admin/users.json', {'pagerNext': 'Suivant {pageNumber}'})
        write(self.root, 'locales/fr/chat/cmd.json', {'added': '@{user} a ajouté {command}', 'err.missing': 'Il manque %s sur %d'})
        write(self.root, 'locales/fr/website/hero.json', {'title': ' '})
        self.assertFails('locales/fr/console/admin/users.json: admin.users.pagerNext: named placeholders differ',
                         'locales/fr/chat/cmd.json: cmd.err.missing: printf placeholders differ',
                         'locales/fr/website/hero.json: hero.title: empty translation')

    def test_arrays_keep_their_length_and_stay_in_console(self):
        write(self.root, 'locales/fr/console/commands.json', {'title': 'Commandes', 'row': {'edit': 'Modifier {name}'}, 'tips': ['Un']})
        write(self.root, 'locales/en/website/hero.json', {'title': 'Hello', 'items': ['a']})
        self.assertFails('locales/fr/console/commands.json: commands.tips: translation changes the array length',
                         'locales/en/website/hero.json: hero.items: use a string; lists are only allowed in console files')

    def test_chat_files_stay_flat(self):
        write(self.root, 'locales/en/chat/loyalty.json', {'points': {'balance': 'Balance'}})
        write(self.root, 'locales/en/chat/mail/beta.json', {'subject': 'Hi'})
        self.assertFails('locales/en/chat/loyalty.json: points: chat files hold flat',
                         'locales/en/chat/mail: chat files cannot live in subfolders')

    def test_empty_english_surface_fails(self):
        write(self.root, 'locales/en/docs/index.json', {})
        (self.root / 'locales/fr/docs/index.json').unlink()
        self.assertFails('locales/en/docs: English catalog must not be empty')


class Status(Fixture):
    def test_status_groups_missing_keys_by_file(self):
        (self.root / 'locales/fr/console/admin/users.json').unlink()
        write(self.root, 'locales/fr/console/lang.json', {'name': 'Français'})
        code, output = run(t.status, self.root, 'fr')
        self.assertEqual(code, 0, output)
        self.assertOutput(output, 'chat: 3/3 keys translated', 'console: 5/7 keys translated',
                          '  locales/fr/console/admin/users.json: 1 missing (new file)',
                          '  locales/fr/console/lang.json: 1 missing\n')
        self.assertNotIn('lang.label', output)
        self.assertIn('  locales/fr/console/lang.json: 1 missing\n    lang.label\n', run(t.status, self.root, 'fr', True)[1])

    def test_status_reports_problems_and_unknown_codes(self):
        write(self.root, 'locales/fr/website/hero.json', '{')
        code, output = run(t.status, self.root, 'fr')
        self.assertEqual((code, 'ERROR locales/fr/website/hero.json:' in output), (1, True))
        with self.assertRaisesRegex(ValueError, 'not registered'):
            t.status(self.root, 'de')


class Scaffold(Fixture):
    def test_scaffold_writes_only_required_keys_where_english_defines_them(self):
        run(t.scaffold, self.root, 'es', 'Español', 'es_ES')
        folder = self.root / 'locales/es'
        created = {path.relative_to(folder).as_posix(): t.read_json(path) for path in sorted(folder.rglob('*.json'))}
        self.assertEqual(created, {
            'console/lang.json': {'name': 'Español'},
            'docs/index.json': {'lang.name': 'Español'},
            'website/lang.json': {'name': 'Español', 'ogLocale': 'es_ES'},
        })
        self.assertEqual((t.locale_codes(self.root), run(t.check, self.root)[0]), (['en', 'fr', 'es'], 0))

    def test_scaffold_follows_nested_english_files(self):
        (self.root / 'locales/en/console/lang.json').unlink()
        (self.root / 'locales/fr/console/lang.json').unlink()
        write(self.root, 'locales/en/console/index.json', {'lang': {'label': 'Language', 'name': 'English'}})
        run(t.scaffold, self.root, 'es', 'Español', 'es_ES')
        self.assertEqual(t.read_json(self.root / 'locales/es/console/index.json'), {'lang': {'name': 'Español'}})

    def snapshot(self):
        return {path: path.read_bytes() for path in sorted(self.root.rglob('*')) if path.is_file()}

    def test_scaffold_refuses_registered_or_existing_languages(self):
        run(t.scaffold, self.root, 'es', 'Español', 'es_ES')
        write(self.root, 'locales/de/website/hero.json', {'title': 'Hallo'})
        before = self.snapshot()
        for code in ('es', 'de'):
            with self.subTest(code=code), self.assertRaisesRegex(ValueError, 'already exists'):
                t.scaffold(self.root, code, 'Other', 'es_ES')
        self.assertEqual(self.snapshot(), before)

    def test_scaffold_rejects_bad_codes_and_names(self):
        for arguments in (('../bad', 'Bad', 'es_ES'), ('it', ' ', 'it_IT'), ('it', 'Italiano', 'italian')):
            with self.subTest(arguments=arguments), self.assertRaises(ValueError):
                t.scaffold(self.root, *arguments)

    def test_new_accepts_positional_and_flag_arguments(self):
        for argv in (['new', 'es', 'Español', 'es_ES'], ['new', 'es', '--name', 'Español', '--og-locale', 'es_ES']):
            with self.subTest(argv=argv):
                self.assertEqual(t.scaffold_arguments(t._parser().parse_args(argv)), ('es', 'Español', 'es_ES'))


class StaticMailTranslations(unittest.TestCase):
    def test_french_templates_preserve_placeholders_and_layout(self):
        class Tags(HTMLParser):
            def __init__(self):
                super().__init__()
                self.tags = []
            def handle_starttag(self, tag, attrs):
                self.tags.append(tag)

        for stem in ('received', 'support-email', 'example-filled'):
            for extension in (('html',) if stem == 'example-filled' else ('html', 'txt')):
                with self.subTest(template=stem, extension=extension):
                    english = (t.ROOT / 'mail' / f'{stem}.{extension}').read_text()
                    french = (t.ROOT / 'mail' / f'{stem}.fr.{extension}').read_text()
                    markers = lambda text: Counter(re.findall(r'\[\[[\s\S]*?\]\]', text))
                    self.assertEqual(markers(french), markers(english))
                    self.assertIn('@itsbagelbot.com', french)
                    self.assertIn('https://discord.gg/SZ2remwSDv', french)
                    if extension == 'html':
                        self.assertIn('lang="fr"', french)
                        a, b = Tags(), Tags()
                        a.feed(english)
                        b.feed(french)
                        self.assertEqual(b.tags, a.tags, 'Translate prose without changing the email layout')


if __name__ == '__main__':
    unittest.main()
