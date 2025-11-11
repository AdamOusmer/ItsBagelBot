from network.handlers import ChatHandler

from utils import Logger

class ChatListener:

    _logger = Logger('System.Network.Listener')

    def __init__(self):
        self.handler = ChatHandler()


    async def register(self, eventsub, broadcaster_id, bot_id):
        """Attach the Twitch chat message event."""
        await eventsub.listen_channel_chat_message(
            broadcaster_user_id=broadcaster_id,
            user_id=bot_id,
            callback=self.handler.dispatch,
        )
        self._logger.info("📝 Registered: channel.chat_message")