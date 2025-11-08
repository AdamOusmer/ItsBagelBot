import requests
from utils import settings


def get_app_token():
    """
    Get an App Access Token via Client Credentials Flow.
    Used for server-to-server requests.
    """
    url = "https://id.twitch.tv/oauth2/token"

    params = {
        "client_id": settings.TWITCH["CLIENT_ID"],
        "client_secret": settings.TWITCH["CLIENT_SECRET"],
        "grant_type": "client_credentials",
    }

    response = requests.post(url, params=params)
    response.raise_for_status()
    return response.json()