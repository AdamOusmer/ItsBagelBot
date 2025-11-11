"""
Bridge module between the application and Twitch's authentication system.
"""

from datetime import datetime, UTC
from utils import settings, Logger
from API.auth import TwitchAuthHelper


class TwitchAuthHandler:

    _logger = Logger("API.Twitch.Auth")

    def __init__(self):
        self.helper = TwitchAuthHelper()
        self.access_token = self.app_token().access_token

#%% OAUTH TOKEN HANDLING

    def oauth_token(self, username: str, isBot: bool = False) -> object:
        """
        Retrieve or refresh an OAuth token for a given username.
        Falls back to full reauthorization if no token is found.
        """
        username = username.lower()

        if isBot:
            username = settings.BOT_LOGIN.lower()

        oauth_token = self.helper.retrive_token("twitch_user_oauth", username)

        if (
                oauth_token is None
                or self._to_datetime(oauth_token.expires_at) <= datetime.now(UTC)
        ):
            self._logger.info(f"🔄 Refreshing or requesting new OAuth token for {username}...")

            if oauth_token is None:
                # No token at all — run browser authorization
                oauth_token = self.helper.request_oauth_token(username, isBot)
            else:
                # Token expired or invalid — refresh
                oauth_token = self.helper.refresh_oauth_token(oauth_token)

        return oauth_token

#%% APP TOKEN HANDLING

    def app_token(self):
        """Retrieve or request a valid app token."""
        token = self.helper.retrive_token("twitch", settings.BOT_LOGIN.lower())

        if token is None or self._to_datetime(token.expires_at) <= datetime.now(UTC):
            self._logger.info("⚙️ No valid app token found, requesting new one...")
            token = self.helper.request_app_token()

        return token

#%% TOKEN REFRESHING

    def refresh_auth_token(self, oauth_token):
        """
        Accepts a Tokens ORM object and returns the refreshed Tokens ORM object.
        """
        if oauth_token is None:
            raise ValueError("Cannot refresh a null token object.")

        self._logger.info(f"🔁 Refreshing auth token for {oauth_token.streamer_name}...")
        return self.helper.refresh_oauth_token(oauth_token)

#%% USER INFO HANDLING
    def get_user_infos(self, username: str):
        """
        Fetch or create a user record (BotUsers) for the given username.
        """
        username = username.lower()
        return self.helper.get_or_create_user_info(username, self.access_token)

#%% HELPERS

    def _to_datetime(self, value):
        """Ensure all timestamps are aware datetimes (UTC)."""
        if isinstance(value, datetime):
            return value if value.tzinfo else value.replace(tzinfo=UTC)

        try:
            dt = datetime.fromisoformat(value)
            if dt.tzinfo is None:
                dt = dt.replace(tzinfo=UTC)
            return dt
        except Exception:
            # Fallback for legacy string formats
            from datetime import datetime as dtmod
            dt = dtmod.strptime(value, "%Y-%m-%d %H:%M:%S")
            return dt.replace(tzinfo=UTC)