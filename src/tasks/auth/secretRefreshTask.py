from datetime import datetime, UTC

from tasks import Task

from utils import SystemBUS

from events.tasks.payloads import SecretRefreshPayload
from events.tasks import EventSecretRefresh

class SecretRefreshTask(Task):
    def __init__(self):
        super().__init__(
            name="Refresh Secret Task",
            description="Handles secret refresh tasks.",
            interval= 60 * 60,  # every 60 minutes
        )

    async def execute(self):
        # Placeholder for actual secret refresh logic

        request = EventSecretRefresh(
            payload= SecretRefreshPayload(
                reason="Periodic secret refresh",
                timestamp=datetime.now(UTC).isoformat(),
            )
        )
        self.amount_executions += 1

        await SystemBUS.publish(request)