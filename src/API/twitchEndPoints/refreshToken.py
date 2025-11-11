import requests
from utils import settings


def refresh_user_token(refresh_token: str):
    """
    Refresh a Twitch OAuth token using the refresh_token.
    """
    url = "https://id.twitch.tv/oauth2/token"

    params = {
        "client_id": settings.TWITCH["CLIENT_ID"],
        "client_secret": settings.TWITCH["CLIENT_SECRET"],
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
    }

    response = requests.post(url, params=params, timeout=10)
    response.raise_for_status()
    return response.json()