import asyncio

from utils import Logger

class Task:
    _logger = Logger(name='System.Task')

    def __init__(self, name, description, interval=50 * 60):


        self.name = name
        self.description = description
        self.amount_executions = 0

        self.running = False

        if not interval or interval <= 0:
            raise ValueError("Interval must be a positive number")

        self.interval = interval  # in seconds

    async def execute(self):
        raise NotImplementedError("Subclasses must implement this method")

    async def start(self):
        self.running = True
        while self.running:
            try:
                await self.execute()
                self._logger.debug(f"✅ '{self.name}' executed successfully.")
            except Exception as e:
                self._logger.debug(f"❌ Error executing task '{self.name}': {e}")
            await asyncio.sleep(self.interval)

    async def stop(self):
        """Placeholder for stopping the task if needed."""
        self.running = False