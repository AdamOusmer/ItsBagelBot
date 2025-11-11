"""
This model defines the TwitchTokens table for storing Twitch API tokens using peewee ORM.
"""
from datetime import datetime, timedelta, UTC
from peewee import CharField, DateTimeField
from database.model import BaseModel

from utils import Logger

_logger = Logger('Database.Models.APITokens')

class APITokens(BaseModel):

    name = CharField()
    streamer_name = CharField(default='unknown', null=True)
    access_token = CharField()
    refresh_token = CharField(null=True)
    expires_at = DateTimeField()

    class Meta:
        table_name = 'api_tokens'
        indexes = (
            (('name', 'streamer_name'), True),
        )

    @classmethod
    def get_token(cls, name: str, streamer_name: str = None):
        if streamer_name:
            return cls.get_or_none((cls.name == name.lower()) & (cls.streamer_name == streamer_name.lower()))

        return cls.get_or_none(cls.name == name.lower())

    def update_from_response(self, data):

        if not data:
            _logger.warning('No data provided to update token.')
            return None

        self.access_token = data["access_token"]
        self.refresh_token = data.get("refresh_token", self.refresh_token)
        self.expires_at = datetime.now(UTC) + timedelta(seconds=data["expires_in"])
        self.save()

        return self

