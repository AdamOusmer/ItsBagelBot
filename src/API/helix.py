import aiohttp

from utils import settings
from API import TWITCHAPI

from utils import EventBUS
from events.tasks import EventChatRequest


class Helix:

    def __init__(self):
        self.broadcaster_id = TWITCHAPI.BROADCASTER['id']
        self.bot_id = TWITCHAPI.BOT['id']

        EventBUS.subscribe(EventChatRequest, self.on_chat_request)


    async def send(self, message):

        url = "https://api.twitch.tv/helix/chat/messages"

        headers = {
            "Authorization": f"Bearer {TWITCHAPI.access_token}",  # App token
            "Client-Id": settings.TWITCH["CLIENT_ID"],
            "Content-Type": "application/json"
        }
        payload = {
            "broadcaster_id": self.broadcaster_id,
            "sender_id": self.bot_id,
            "message": message
        }

        async with aiohttp.ClientSession() as s:
            async with s.post(url, headers=headers, json=payload) as r:
                print(await r.json())


    async def on_chat_request(self, event: EventChatRequest):
        message = event.payload["message"]

        parts = message.split("\n")

        if not parts:
            print("No message parts to send.")
            return

        for part in parts:
            await self.send(part)
