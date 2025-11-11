"""
Centralized application settings.

Uses environment variables (managed via Doppler or .env) and provides
a singleton `settings` instance for easy access across the project.
"""

import os
from datetime import datetime, UTC
from twitchAPI.type import AuthScope

from utils import SystemBUS
from events.tasks import EventSecretRefresh

from utils import Logger


class Settings:
    """Application settings and configurations."""

    def __init__(self):
        self._load_secrets()
        self._load_constants()

        self.logger = Logger(name='System.Settings')

        SystemBUS.subscribe(EventSecretRefresh, self.reload)


    def _load_constants(self):
        self.WAKE_UP_TIME = datetime.now(UTC)

        self.BOT_NAME = 'ItsBagelBot'
        self.BOT_LOGIN = self.BOT_NAME.lower()


        self.SCOPES = {
            'bot': [
                AuthScope.CHAT_READ,
                AuthScope.CHAT_EDIT,
                AuthScope.USER_READ_CHAT,
                AuthScope.USER_WRITE_CHAT,
                AuthScope.USER_BOT,
                AuthScope.CHANNEL_BOT,
                AuthScope.MODERATOR_READ_FOLLOWERS,
            ],
            'broadcaster': [
                AuthScope.MODERATOR_READ_FOLLOWERS,
                AuthScope.USER_READ_CHAT,
                AuthScope.USER_WRITE_CHAT,
            ],
        }


    def _load_secrets(self):
        """Reload all environment-dependent configurations."""
        self.MAIN_BROADCASTER = os.getenv('MAIN_BROADCASTER', 'itsmavey').lower()
        self.SPECIAL_ID = os.getenv('SPECIAL_ID')

        self.CONTACT = {
            'EMAIL': os.getenv('CONTACT_EMAIL', "contact@itsmavey.com"),
            'DISCORD': os.getenv('CONTACT_DISCORD', "https://discord.gg/SZ2remwSDv"),
        }

        self.DATABASE_ENGINE = os.getenv("DATABASE_ENGINE", "sqlite")
        self.DATABASE = {
            'postgres': {
                "NAME": os.getenv("POSTGRES_DB"),
                "ENGINE": "postgres",
                "USER": os.getenv("POSTGRES_USER"),
                "PASSWORD": os.getenv("POSTGRES_PASSWORD", ""),
                "HOST": os.getenv("POSTGRES_HOST"),
                "PORT": int(os.getenv("POSTGRES_PORT", 5432)),
            },
            'sqlite': {
                "NAME": "bagelbot.db",
                "ENGINE": "sqlite",
            },
        }

        self.SPOTIFY = {
            'CLIENT_ID': os.getenv('SPOTIFY_CLIENT_ID'),
            'CLIENT_SECRET': os.getenv('SPOTIFY_CLIENT_SECRET'),
            'REDIRECT_URI': os.getenv('SPOTIFY_REDIRECT_URI', 'http://127.0.0.1:8080'),
        }

        self.TWITCH = {
            'CLIENT_ID': os.getenv('TWITCH_CLIENT_ID'),
            'CLIENT_SECRET': os.getenv('TWITCH_CLIENT_SECRET'),
            'REDIRECT_URI': os.getenv('TWITCH_REDIRECT', 'http://localhost:17563'),
        }



    async def reload(self, event: EventSecretRefresh):
        """Reload settings on secret refresh event."""
        self._load_secrets()
        self.logger.debug("🌷 Secrets reloaded.")