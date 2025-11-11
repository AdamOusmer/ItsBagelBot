# commands/base.py
from commands import COMMAND_REGISTRY

from utils import Logger


class Component:
    """Base class for all grouped commands (auto-binds registered methods)."""

    _logger = Logger("System.Commands.Component")

    def __init_subclass__(cls, **kwargs):
        """Instantiate and bind class-based commands."""
        super().__init_subclass__(**kwargs)
        instance = cls()

        for attr_name in dir(instance):
            method = getattr(instance, attr_name)
            meta = getattr(method, "__command_meta__", None)

            if not meta:
                continue  # not a command

            cmd_name = meta["name"]

            if cmd_name in COMMAND_REGISTRY:
                # Update existing entry with bound method
                COMMAND_REGISTRY[cmd_name]["func"] = method
                COMMAND_REGISTRY[cmd_name]["component"] = instance

                cls._logger.debug(f"🔗 Bound command '{cmd_name}' to {cls.__name__}")

        cls._logger.info(f"✅ Loaded command module: {cls.__name__}")