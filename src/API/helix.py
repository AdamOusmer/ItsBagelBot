from API import TWITCHAPI

from utils import EventBUS, Logger
from events.tasks import EventChatRequest

from API.twitchEndPoints.helix.chatMessages import sendMessage


class Helix:

    _logger = Logger('API.Twitch.Helix')

    def __init__(self):
        self.broadcaster_id = TWITCHAPI.BROADCASTER['id']
        self.bot_id = TWITCHAPI.BOT['id']

        EventBUS.subscribe(EventChatRequest, self.on_chat_request)


    def on_chat_request(self, event: EventChatRequest):
        message = event.payload["message"]

        parts = message.split("\n")

        if not parts:
            self._logger.debug("No message parts to send.")
            return

        for part in parts:
            sendMessage(part)
