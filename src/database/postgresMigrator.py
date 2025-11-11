"""
PostgreSQL-specific migration engine for BagelBot.
Provides schema inspection and simple add-column migrations.
"""

from peewee import Model
from playhouse.migrate import PostgresqlMigrator, migrate
from database import db

from utils import Logger


_logger = Logger('System.Database.Migrator')

def _get_existing_columns(table_name: str) -> set[str]:
    """Fetch all existing columns for a table from Postgres system tables."""
    cursor = db.cursor()
    cursor.execute(
        """
        SELECT column_name
        FROM information_schema.columns
        WHERE table_name = %s
          AND table_schema = 'public';
        """,
        (table_name,),
    )
    return {row[0] for row in cursor.fetchall()}


def makemigrations(models: list[Model]):
    """
    Compare Peewee models with the database schema and print missing columns.
    Like Django's 'makemigrations' but for Postgres only.
    """
    _logger.info("🔍 Checking PostgreSQL schema differences...")

    cursor = db.cursor()
    for model in models:
        table_name = model._meta.table_name

        # If table doesn’t exist, mark for creation
        if not db.table_exists(table_name):
            print(f"🆕 Table '{table_name}' does not exist and will be created.")
            continue

        # Compare model fields with existing columns
        existing_cols = _get_existing_columns(table_name)
        for field_name, field_obj in model._meta.fields.items():
            if field_name not in existing_cols:
                _logger.debug(
                    f"🆕 Will add column '{field_name}' to table '{table_name}' ({type(field_obj).__name__})"
                )

    _logger.info("✅ Schema check complete.")


def migrate_models(models: list[Model]):
    """
    Apply schema migrations for PostgreSQL.
    Automatically creates missing tables and columns.
    """
    _logger.info("⚙️ Running PostgreSQL migrations...")
    migrator = PostgresqlMigrator(db)
    operations = []

    for model in models:
        table_name = model._meta.table_name

        # --- Table creation ---
        if not db.table_exists(table_name):
            _logger.debug(f"🆕 Creating table '{table_name}' for model {model.__name__}")
            model.create_table()
            continue

        # --- Add missing columns ---
        existing_cols = _get_existing_columns(table_name)
        for field_name, field_obj in model._meta.fields.items():
            if field_name not in existing_cols:
                _logger.debug(f"🆕 Adding column '{field_name}' to '{table_name}'")
                operations.append(migrator.add_column(table_name, field_name, field_obj))

    # --- Apply migrations if any ---
    if operations:
        with db.atomic():
            migrate(*operations)
        _logger.info(f"✅ Applied {len(operations)} PostgreSQL migration(s).")
    else:
        _logger.info("✅ No migrations needed.")