import commands.decorators as cmd

from utils import settings
from datetime import datetime, UTC

from commands.components import Component

class Default(Component):

    def __init__(self):
        self.component_name = "Default Commands"

    @cmd.command(name='bagel', aliases=['bagels'], description='Bagel commands')
    async def bagel(self, ctx):
        return f"🥯 @{ctx.user} here a bagel for you"

    @cmd.command(name='ping', aliases=[], description='Ping commands')
    async def ping(self, ctx):
        return "Pong 🏓"


    @cmd.command(name="uptime", description="Shows how long the bot has been running.")
    async def uptime(self, ctx):
        now = datetime.now(UTC)
        delta = now - settings.WAKE_UP_TIME

        # Format delta nicely
        hours, remainder = divmod(int(delta.total_seconds()), 3600)
        minutes, seconds = divmod(remainder, 60)

        parts = []
        if hours > 0:
            parts.append(f"{hours}h")
        if minutes > 0 or hours > 0:
            parts.append(f"{minutes}m")
        parts.append(f"{seconds}s")

        return f"{settings.BOT_NAME} as been on for: {' '.join(parts)}"

    @cmd.command(name='help', aliases=[], description='Help commands')
    async def help(self, ctx):
        return f"@{ctx.user} Email: {settings.CONTACT['EMAIL']} \n @{ctx.user} Discord: {settings.CONTACT['DISCORD']}"
