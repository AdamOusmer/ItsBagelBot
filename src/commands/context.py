from events.tasks import EventChatRequest
from events.tasks.payloads import ChatRequestPayload
from utils import EventBUS, Logger

from payload import Permission

class Context:

    _logger = Logger('System.Commands.Context')
    """Lightweight context object passed to commands."""
    def __init__(self, user, id, message, permission: Permission , send_func=None):
        self.user = user
        self.id = id
        self.raw_message = message

        if message.startswith("!"):

            content = message[1:].strip()

            if not content: # if message is just "!"
                self.command = None
                self.message = ""
            else:
                parts = content.split(maxsplit=1)
                self.command = parts[0].lower()
                self.message = parts[1] if len(parts) > 1 else ""

        else:
            self.command = None
            self.message = message.strip()

        self.send_func = send_func

        # Permission flags from Twitch badges
        self.is_broadcaster = permission["is_broadcaster"]
        self.is_mod = permission["is_mod"]
        self.is_vip = permission["is_vip"]
        self.is_subscriber = permission["is_subscriber"]

    async def send(self, text: str):
        """Send a message back to chat using either the custom sender or Helix."""
        if self.send_func:
            # Use the provided send function (e.g., test or IRC-level sender)
            try:
                if callable(self.send_func):
                    result = self.send_func(text)
                    if hasattr(result, "__await__"):
                        await result
                else:
                    self._logger.warning(f"send_func is not callable: {self.send_func}")
            except Exception as e:
                self._logger.error(f"send_func failed: {e}")

            return

        request_event = EventChatRequest(
            payload=ChatRequestPayload(
                message=text,
            )
        )

        await EventBUS.publish(request_event)