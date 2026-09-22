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

    def fixture(self, root):
        for directory in t.CATALOGS.values():
            folder = root / directory
            folder.mkdir(parents=True)
            (folder / 'en.json').write_text('{"hello":"Hello {user}"}')
        (root / t.MANIFEST).write_text('["en", "fr"]')

    def test_partial_locale_warns_but_strict_locale_fails(self):
        with tempfile.TemporaryDirectory() as folder, contextlib.redirect_stdout(io.StringIO()):
            root = Path(folder)
            self.fixture(root)
            self.assertEqual(t.check(root), 0)
            self.assertEqual(t.check(root, ['fr']), 1)

    def test_unregistered_locale_fails(self):
        with tempfile.TemporaryDirectory() as folder, contextlib.redirect_stdout(io.StringIO()):
            root = Path(folder)
            self.fixture(root)
            (root / t.CATALOGS['chat'] / 'es.json').write_text('{}')
            self.assertEqual(t.check(root), 1)

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

    def test_scaffold_preserves_existing_work_and_registers_partial_locale(self):
        with tempfile.TemporaryDirectory() as folder, contextlib.redirect_stdout(io.StringIO()):
            root = Path(folder)
            self.fixture(root)
            t.scaffold(root, 'es', 'Español', 'es_ES')
            self.assertIn('es', t.locale_codes(root))
            path = root / t.CATALOGS['console'] / 'es.json'
            self.assertEqual(t.read_json(path), {'lang': {'name': 'Español'}})
            before = path.read_bytes()
            with self.assertRaisesRegex(ValueError, 'already exists'):
                t.scaffold(root, 'es', 'Other', 'es_ES')
            self.assertEqual(path.read_bytes(), before)
            with self.assertRaises(ValueError):
                t.scaffold(root, '../bad', 'Bad', 'es_ES')


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
