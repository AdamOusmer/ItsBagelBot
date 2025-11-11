import asyncio
from twitchAPI.twitch import Twitch
from twitchAPI.eventsub.websocket import EventSubWebsocket

from network.listeners import LISTENERS
from utils import settings, Logger
from API import TWITCHAPI

import time


class EventSub:

    RECONNECT_TIMEOUT = 60  # seconds
    _logger = Logger(name='System.Network.EventSub')

    def __init__(self, bot_client: Twitch):
        self.bot_client = bot_client
        self.broadcaster_id = TWITCHAPI.BROADCASTER['id']
        self.bot_id = TWITCHAPI.BOT['id']
        self.eventsub: EventSubWebsocket | None = None

        self._last_connected = time.time()

    @classmethod
    async def create(cls):
        # Twitch instance; you’re already handling auth via TWITCHAPI
        twitch = await Twitch(settings.TWITCH['CLIENT_ID'], authenticate_app=False)

        await twitch.set_user_authentication(
            TWITCHAPI.BOT['oauth_token'],
            settings.SCOPES['bot'],
            TWITCHAPI.BOT['refresh_token'],
        )

        cls._logger.info(f"🎥 Broadcaster authenticated as {TWITCHAPI.BROADCASTER['display_name']}")
        cls._logger.info(f"🤖 Bot authenticated as {TWITCHAPI.BOT['display_name']}")

        return cls(twitch)

    async def register_listeners(self):
        for listener in LISTENERS:
            try:
                await listener.register(
                    eventsub=self.eventsub,
                    broadcaster_id=self.broadcaster_id,
                    bot_id=self.bot_id,
                )
                self._logger.debug(f"✅ Registered listener: {listener.__class__.__name__}")
            except Exception as e:
                self._logger.debug(f"⚠️ Failed to register {listener.__class__.__name__}: {e}")

    async def start_eventsub(self):
        """Start EventSubWebsocket once."""
        self._logger.debug("🔌 Starting EventSub WebSocket")
        self.eventsub = EventSubWebsocket(self.bot_client)

        self.eventsub.start()

        await self.register_listeners()

    async def _monitor(self):
        """Watchdog loop for EventSub connection state."""
        while True:
            await asyncio.sleep(10)

            if not self.eventsub:
                continue

            session = self.eventsub.active_session
            if session is None:
                self._logger.debug("⚠️ EventSub session missing — restarting...")
                await self._restart()
                continue

            # Track connection status
            if session.status == "connected":
                self._last_connected = time.time()

            elif session.status == "reconnecting":
                elapsed = time.time() - self._last_connected
                if elapsed > self.RECONNECT_TIMEOUT:
                    self._logger.debug(f"⏰ Reconnect taking too long ({elapsed:.1f}s) — forcing restart")
                    await self._restart()
            else:
                self._logger.debug(f"⚠️ EventSub status = {session.status}, restarting...")
                await self._restart()

    async def _restart(self):
        """Stop (if running) and start a new EventSubWebsocket."""
        if self.eventsub is not None:
            try:
                await self.eventsub.stop()
            except Exception as e:
                self._logger.debug(f"⚠️ Error while stopping EventSub: {e}")

        await asyncio.sleep(2)

        await self.start_eventsub()

    async def start(self):
        await self.start_eventsub()

        # background monitor to kick it if it silently dies
        asyncio.create_task(self._monitor())

        await asyncio.Future()


async def main():
    eventsub = await EventSub.create()
    await eventsub.start()


if __name__ == "__main__":
    asyncio.run(main())