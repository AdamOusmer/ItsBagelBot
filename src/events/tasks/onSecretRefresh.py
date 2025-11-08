from dataclasses import dataclass
from typing import Literal
from events.Event import BaseEvent

from events.tasks.payloads import SecretRefreshPayload

@dataclass
class EventSecretRefresh(BaseEvent):
    name: Literal["tasks.secrets.refresh"] = "tasks.secrets.refresh"
    source: Literal["system"] = "system"
    payload: SecretRefreshPayload = None