"""
This module will have all the migration scripts for the database.
"""

import inspect
from peewee import Model, Field
from playhouse.migrate import SqliteMigrator, migrate
from database import db

from utils import Logger


_logger = Logger('System.Database.Migrator')

def makemigrations(models: list[Model]):
    """
    Compares model definitions with the database schema
    and prints what would change (like Django's makemigrations).
    """
    migrator = SqliteMigrator(db)
    cursor = db.cursor()

    _logger.info("🔍 Checking for schema differences...")
    for model in models:
        table_name = model._meta.table_name
        existing_cols = {
            row[1] for row in cursor.execute(f"PRAGMA table_info({table_name});")
        }

        for field_name, field_obj in model._meta.fields.items():
            if field_name not in existing_cols:
                _logger.debug(f"🆕 Will add column '{field_name}' to table '{table_name}' ({type(field_obj).__name__})")
    _logger.info("✅ Done checking schema.")


def migrate_models(models: list[Model]):
    """
    Actually applies the missing columns (like Django's migrate).
    Adds new fields but won't delete or rename.
    """
    migrator = SqliteMigrator(db)
    cursor = db.cursor()
    operations = []

    for model in models:
        table_name = model._meta.table_name

        # 👇 CHECK HERE for new models (missing tables)
        if not db.table_exists(table_name):
            _logger.debug(f"🆕 Creating new table '{table_name}' for model {model.__name__}")
            model.create_table()
            continue  # skip column check since table is brand new

        # --- Existing table: check for missing columns ---
        existing_cols = {
            row[1] for row in cursor.execute(f"PRAGMA table_info({table_name});")
        }

        for field_name, field_obj in model._meta.fields.items():
            if field_name not in existing_cols:
                _logger.debug(f"🆕 Adding column '{field_name}' to '{table_name}'")
                operations.append(migrator.add_column(table_name, field_name, field_obj))

    # --- Apply column migrations ---
    if operations:
        _logger.info(f"✍️ Applying {len(operations)} migration(s)...")
        with db.atomic():
            migrate(*operations)
        _logger.info("✅ Migration complete.")
    else:
        _logger.info("✅ No migrations needed.")