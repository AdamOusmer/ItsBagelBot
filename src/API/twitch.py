from API.auth import TwitchAuthHandler
from utils import settings

from utils import SystemBUS
from events.tasks import EventAuthRefresh


class TwitchAPI:
    def __init__(self, channel: str):
        if not channel or channel.strip() == "":
            raise ValueError("Channel must be provided.")

        self.channel = channel
        self.auth = TwitchAuthHandler()

        # App token (client credentials)
        self.access_token = self.auth.app_token().access_token

        # User tokens
        self._broadcaster_token = self.auth.oauth_token(self.channel, isBot=False)
        self._bot_token = self.auth.oauth_token(settings.BOT_LOGIN, isBot=True)

        # Retrieve or create user info (returns BotUsers object)
        broadcaster_info = self.auth.get_user_infos(self.channel)
        bot_info = self.auth.get_user_infos(settings.BOT_LOGIN)

        # Store broadcaster data
        self.BROADCASTER = {
            'oauth_token': self._broadcaster_token.access_token,
            'refresh_token': self._broadcaster_token.refresh_token,
            'id': broadcaster_info.user_id,
            'name': broadcaster_info.username,
            'display_name': broadcaster_info.display_name,
        }

        # Store bot data
        self.BOT = {
            'oauth_token': self._bot_token.access_token,
            'refresh_token': self._bot_token.refresh_token,
            'id': bot_info.user_id,
            'name': bot_info.username,
            'display_name': bot_info.display_name,
        }

        SystemBUS.subscribe(EventAuthRefresh, self.refresh_tokens)

    def refresh_tokens(self, event: EventAuthRefresh):
        """Refresh both bot and broadcaster OAuth tokens if needed."""

        print("Refreshing bot tokens...")

        oauth_token = self.auth.refresh_auth_token(self._broadcaster_token)
        bot_oauth_token = self.auth.refresh_auth_token(self._bot_token)

        self.BOT['oauth_token'] = bot_oauth_token.access_token
        self.BOT['refresh_token'] = bot_oauth_token.refresh_token

        self.BROADCASTER['oauth_token'] = oauth_token.access_token
        self.BROADCASTER['refresh_token'] = oauth_token.refresh_token

