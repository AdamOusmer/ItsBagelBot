import requests

from API import TWITCHAPI

from utils import settings


def send(message, broadcaster_id=None):

    url = "https://api.twitch.tv/helix/chat/messages"

    headers = {
        "Authorization": f"Bearer {TWITCHAPI.access_token}",  # App token
        "Client-Id": settings.TWITCH["CLIENT_ID"],
        "Content-Type": "application/json"
    }

    payload = {
        "broadcaster_id": broadcaster_id or TWITCHAPI.BROADCASTER['id'],
        "sender_id": TWITCHAPI.BOT['id'],
        "message": message
    }

    response = requests.post(url, headers=headers, json=payload, timeout=10)

    response.raise_for_status()
