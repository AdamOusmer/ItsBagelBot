from typing import TypedDict

class SecretRefreshPayload(TypedDict):
    reason: str
    timestamp: str
