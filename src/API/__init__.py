from API.spotify import SpotifyAPI
from API.twitch import TwitchAPI

from utils import settings


TWITCHAPI = TwitchAPI(settings.MAIN_BROADCASTER)

SPOTIFYAPI = SpotifyAPI()

from API.helix import Helix

HELIX = Helix()