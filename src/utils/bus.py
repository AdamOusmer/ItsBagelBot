"""
A simple event bus implementation for asynchronous event handling.
"""

import asyncio
from typing import Type, Callable, Dict, List, Any
from events import BaseEvent


class Bus:
    """A lightweight async/sync event bus for inter-module communication."""

    def __init__(self):
        # Dict mapping event classes to list of callbacks
        self._subscribers: Dict[Type[BaseEvent], List[Callable[[BaseEvent], Any]]] = {}

    def subscribe(self, event_type: Type[BaseEvent], callback: Callable[[BaseEvent], Any]):
        """Register a callback for a specific event type (class-based)."""
        if event_type not in self._subscribers:
            self._subscribers[event_type] = []
        self._subscribers[event_type].append(callback)
        print(f"✅ Subscribed to {event_type.__name__}: {callback.__name__}")

    async def publish(self, event: BaseEvent):
        """Publish an event to all registered subscribers."""
        event_type = type(event)
        subscribers = self._subscribers.get(event_type, [])
        if not subscribers:
            print(f"⚠️ No subscribers for {event_type.__name__}")
            return

        print(f"📣 Publishing {event_type.__name__} to {len(subscribers)} subscriber(s)...")

        tasks = []

        for callback in subscribers:
            if asyncio.iscoroutinefunction(callback):
                tasks.append(asyncio.create_task(callback(event)))
            else:
                tasks.append(asyncio.to_thread(callback, event))

        await asyncio.gather(*tasks)