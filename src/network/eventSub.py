import asyncio

from twitchAPI.twitch import Twitch
from twitchAPI.eventsub.websocket import EventSubWebsocket

from network.listeners import LISTENERS

from utils import settings

from API import TWITCHAPI



class EventSub:
    def __init__(self, bot_client):
        self.bot_client = bot_client
        self.broadcaster_id = TWITCHAPI.BROADCASTER['id']
        self.bot_id = TWITCHAPI.BOT['id']
        self.eventsub = None

    @classmethod
    async def create(cls):
        """Initialize both Twitch clients — broadcaster & bot."""
        bot_client = await Twitch(settings.TWITCH['CLIENT_ID'], authenticate_app=False)


        await bot_client.set_user_authentication(
            TWITCHAPI.BOT['oauth_token'],  # bot token
            settings.SCOPES['bot'],
            TWITCHAPI.BOT['refresh_token']
        )


        print(f"✅ Broadcaster authenticated as {TWITCHAPI.BROADCASTER['display_name']}")
        print(f"🤖 Bot authenticated as {TWITCHAPI.BOT['display_name']}")

        return cls(bot_client)


    async def register_listeners(self):
        """Register all event listeners."""
        for listener in LISTENERS:
            try:
                await listener.register(
                    eventsub=self.eventsub,
                    broadcaster_id=self.broadcaster_id,
                    bot_id=self.bot_id
                )

                print(f"✅ Registered listener: {listener.__class__.__name__}")

            except Exception as e:
                print(f"⚠️ Failed to register {listener.__class__.__name__}: {e}")


    async def start(self):
        """Start the EventSub WebSocket and listen for events."""
        self.eventsub = EventSubWebsocket(self.bot_client)
        self.eventsub.start()

        await self.register_listeners()


        await asyncio.Future()  # keep alive


async def main():
    bot = await EventSub.create()
    await bot.start()


if __name__ == "__main__":
    asyncio.run(main())