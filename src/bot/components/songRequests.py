from commands import decorators as cmd
from commands.components import Component

from API import SPOTIFYAPI


class Spotify(Component):

    def __init__(self):
        self.component_name = "Spotify Commands"


    @cmd.command(name='songrequest', aliases=['sr'], description='Request a song to be played on stream via Spotify')
    async def songrequest(self, ctx):
        query =  ctx.message.strip()

        if not query or query == '':
            return f"@{ctx.user} Please provide a song name or artist to request."

        return SPOTIFYAPI.search_to_queue(query)

    @cmd.mod
    @cmd.command(name='skip', aliases=['next'], description='Skip the current song playing on stream')
    async def skip(self, ctx):
        return SPOTIFYAPI.next_track()

    @cmd.mod
    @cmd.command(name='previous', aliases=['prev'], description='Play the previous song on Spotify')
    async def previous(self, ctx):
        return SPOTIFYAPI.previous_track()

    @cmd.mod
    @cmd.command(name='play', aliases=[], description='Resume playback on Spotify')
    async def play(self, ctx):
        return SPOTIFYAPI.play()

    @cmd.mod
    @cmd.command(name='pause', aliases=[], description='Pause playback on Spotify')
    async def pause(self, ctx):
        return SPOTIFYAPI.pause()


    @cmd.mod
    @cmd.command(name='volume', aliases=['vol'], description='Set the volume on Spotify (0-100)')
    async def volume(self, ctx):
        try:
            volume_percent = int(ctx.message.strip())
        except ValueError:
            return f"@{ctx.user} Please provide a valid volume percentage (0-100)."

        if volume_percent < 0 or volume_percent > 100:
            return f"@{ctx.user} Volume must be between 0 and 100."

        return SPOTIFYAPI.volume(volume_percent)