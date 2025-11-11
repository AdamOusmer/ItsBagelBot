import asyncio
from twitchAPI.twitch import Twitch
from twitchAPI.eventsub.websocket import EventSubWebsocket

from network.listeners import LISTENERS
from utils import settings
from API import TWITCHAPI

import time


class EventSub:

    RECONNECT_TIMEOUT = 60  # seconds

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

        print(f"✅ Broadcaster authenticated as {TWITCHAPI.BROADCASTER['display_name']}")
        print(f"🤖 Bot authenticated as {TWITCHAPI.BOT['display_name']}")

        return cls(twitch)

    async def register_listeners(self):
        for listener in LISTENERS:
            try:
                await listener.register(
                    eventsub=self.eventsub,
                    broadcaster_id=self.broadcaster_id,
                    bot_id=self.bot_id,
                )
                print(f"✅ Registered listener: {listener.__class__.__name__}")
            except Exception as e:
                print(f"⚠️ Failed to register {listener.__class__.__name__}: {e}")

    async def start_eventsub(self):
        """Start EventSubWebsocket once."""
        print("🔌 Starting EventSub WebSocket...")
        self.eventsub = EventSubWebsocket(self.bot_client)

        # optional: tweak reconnect delays (seconds)
        # self.eventsub.reconnect_delay_steps = [2, 5, 10, 30]

        self.eventsub.start()  # runs in its own thread

        # MUST subscribe within 10s or Twitch will drop you
        await self.register_listeners()

    async def _monitor(self):
        """Watchdog loop for EventSub connection state."""
        while True:
            await asyncio.sleep(10)

            if not self.eventsub:
                continue

            session = self.eventsub.active_session
            if session is None:
                print("⚠️ EventSub session missing — restarting...")
                await self._restart()
                continue

            # Track connection status
            if session.status == "connected":
                self._last_connected = time.time()

            elif session.status == "reconnecting":
                elapsed = time.time() - self._last_connected
                if elapsed > self.RECONNECT_TIMEOUT:
                    print(f"⏰ Reconnect taking too long ({elapsed:.1f}s) — forcing restart")
                    await self._restart()
            else:
                # 'disconnected' or any other
                print(f"⚠️ EventSub status = {session.status}, restarting...")
                await self._restart()

    async def _restart(self):
        """Stop (if running) and start a new EventSubWebsocket."""
        if self.eventsub is not None:
            try:
                await self.eventsub.stop()
            except Exception as e:
                print(f"⚠️ Error while stopping EventSub: {e}")

        await asyncio.sleep(2)

        await self.start_eventsub()

    async def start(self):
        await self.start_eventsub()

        # background monitor to kick it if it silently dies
        asyncio.create_task(self._monitor())

        # keep your app alive; your real app loop/logic can go here
        await asyncio.Future()  # or your own run loop


async def main():
    eventsub = await EventSub.create()
    await eventsub.start()


if __name__ == "__main__":
    asyncio.run(main())