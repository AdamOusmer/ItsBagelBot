import requests
from utils import settings


def get_user_token(code: str):
    """
    Exchange authorization code for user access token.
    This is part of the OAuth Authorization Code Flow.
    """
    url = "https://id.twitch.tv/oauth2/token"

    params = {
        "client_id": settings.TWITCH["CLIENT_ID"],
        "client_secret": settings.TWITCH["CLIENT_SECRET"],
        "code": code,
        "grant_type": "authorization_code",
        "redirect_uri": settings.TWITCH["REDIRECT_URI"],
    }

    response = requests.post(url, data=params)
    response.raise_for_status()
    return response.json()