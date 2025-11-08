from peewee import SqliteDatabase, PostgresqlDatabase
from utils import settings

DATABASES = {
    "sqlite": lambda: SqliteDatabase(
        settings.DATABASE["sqlite"]["NAME"],
        pragmas={
            "journal_mode": "wal",
            "cache_size": -1024 * 64,
            "foreign_keys": 1,
        },
    ),

    "postgres": lambda: PostgresqlDatabase(
        settings.DATABASE["postgres"]["NAME"],
        user=settings.DATABASE["postgres"]["USER"],
        password=settings.DATABASE["postgres"]["PASSWORD"],
        host=settings.DATABASE["postgres"]["HOST"],
        port=int(settings.DATABASE["postgres"]["PORT"]),
    ),
}

# fallback to sqlite if DATABASE_ENGINE is invalid
db = DATABASES.get(settings.DATABASE_ENGINE, DATABASES["sqlite"])()