import requests
from utils import settings


def get_user_infos(username: str, access_token: str):
    """
    Get Twitch user info by username.
    Requires a valid access token (app or user).
    """
    if not username:
        raise ValueError("Username must be provided.")

    url = "https://api.twitch.tv/helix/users"
    headers = {
        "Client-ID": settings.TWITCH["CLIENT_ID"],
        "Authorization": f"Bearer {access_token}",
    }
    params = {"login": username}

    response = requests.get(url, headers=headers, params=params, timeout=10)
    response.raise_for_status()

    data = response.json()
    if "data" not in data or not data["data"]:
        raise ValueError(f"No user found for username: {username}")

    return data["data"][0]


def get_user_by_id(user_id: str, access_token: str):
    """
    Get Twitch user info by user ID.
    Requires a valid access token (app or user).
    """
    if not user_id:
        raise ValueError("User ID must be provided.")

    url = "https://api.twitch.tv/helix/users"
    headers = {
        "Client-ID": settings.TWITCH["CLIENT_ID"],
        "Authorization": f"Bearer {access_token}",
    }
    params = {"id": user_id}

    response = requests.get(url, headers=headers, params=params, timeout=10)
    response.raise_for_status()

    data = response.json()
    if "data" not in data or not data["data"]:
        raise ValueError(f"No user found for ID: {user_id}")

    return data["data"][0]