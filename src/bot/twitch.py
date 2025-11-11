from events.twitch import EventChatCommand, EventChatMessage
from events.tasks import EventChatRequest
from events.tasks.payloads import ChatRequestPayload

from utils import EventBUS

from utils import settings, Logger

from commands import COMMAND_MANAGER, Context

class TwitchBot:

    _logger = Logger('TwitchBot')
    def __init__(self):
        # Subscribe using class references, not strings
        EventBUS.subscribe(EventChatCommand, self.on_command)
        EventBUS.subscribe(EventChatMessage, self.on_message)

        # Create a single shared command manager
        self.command_manager = COMMAND_MANAGER
        self.special_id_first = True

    async def on_command(self, event: EventChatCommand):
        payload = event.payload
        username = payload["username"]
        message = payload["command"]
        user_id = payload["user_id"]

        self._logger.info(f"Command received from {username}: {message}")

        # Create context for this command
        ctx = Context(
            user=username,
            message=message,
            permission=payload["permission"],
            id=user_id,
        )

        # Let the command manager handle dispatching
        await self.command_manager.dispatch(ctx)

    async def on_message(self, event: EventChatMessage):
        username = event.payload["username"]
        message = event.payload["message"]
        user_id = event.payload["user_id"]

        if user_id == settings.SPECIAL_ID:
            if self.special_id_first:
                self._logger.info("🚨 Special ID message detected! Responding with bagels...")
                self.special_id_first = False

                request_event = EventChatRequest(
                    payload=ChatRequestPayload(
                        message="🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯\n🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯\n🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯🥯",
                    )
                )

                await EventBUS.publish(request_event)

        self._logger.info(f"💬 Message from {username}: {message}")