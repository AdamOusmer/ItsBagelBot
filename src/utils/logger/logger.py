from utils.logger import config
import logging

_VALID_LEVELS = {
    'DEBUG': logging.DEBUG,
    'INFO': logging.INFO,
    'WARNING': logging.WARNING,
    'ERROR': logging.ERROR,
    'CRITICAL': logging.CRITICAL,
    'NOTSET': logging.NOTSET
}

_COLORS = {
    'DEBUG': '\033[94m',   # bright blue
    'INFO': '\033[92m',    # bright green
    'WARNING': '\033[93m', # yellow
    'ERROR': '\033[91m',   # red
    'CRITICAL': '\033[95m', # magenta

    'None': '\033[0m'      # reset
}


class Logger:

    def __init__(self, name: str = config.LOGGER_NAME, level: str = config.LOGGER_LEVEL):
        if not name or not name.strip():
            raise ValueError("Name cannot be None or empty")

        if not level or not level.strip():
            raise ValueError("Level cannot be None or empty")

        if level not in _VALID_LEVELS:
            raise ValueError(f"Invalid level: {level}")

        self.name = name
        self.level = _VALID_LEVELS[level]
        self._logger = logging.getLogger(self.name)

        if not self._logger.handlers:
            handler = logging.StreamHandler()
            fmt = "%(message)s" if self.level == logging.NOTSET else config.LOGGER_FORMAT
            handler.setFormatter(logging.Formatter(fmt))

            self._logger.addHandler(handler)
            self._logger.setLevel(self.level)

    def _format_message(self, message: str, level = 'None') -> str:
        """Format message only if logging level is NOTSET"""
        if self.level == logging.NOTSET:
            return message
        else:
            return f"\t{_COLORS.get(level, 'None')}{message}{_COLORS['None']}"

    def debug(self, message: str) -> None:
        if not message or message.strip() == "":
            raise ValueError("Message cannot be None or empty")

        self._logger.debug(self._format_message(message, level='DEBUG'))

    def info(self, message: str) -> None:
        if not message or message.strip() == "":
            raise ValueError("Message cannot be None or empty")

        self._logger.info(self._format_message(message, level='INFO'))

    def warning(self, message: str) -> None:
        if not message or message.strip() == "":
            raise ValueError("Message cannot be None or empty")

        self._logger.warning(self._format_message(message, level='WARNING'))

    def error(self, message: str) -> None:
        if not message or message.strip() == "":
            raise ValueError("Message cannot be None or empty")

        message = "\033[91m" + message + "\033[0m"
        self._logger.error(self._format_message(message, level='ERROR'))

    def critical(self, message: str) -> None:
        if not message or message.strip() == "":
            raise ValueError("Message cannot be None or empty")

        message = "\033[91m" + message + "\033[0m"
        self._logger.critical(self._format_message(message, level='CRITICAL'))

    def exception(self, message: str) -> None:
        if not message or message.strip() == "":
            raise ValueError("Message cannot be None or empty")

        message = "\033[91m" + message + "\033[0m"
        self._logger.exception(self._format_message(message, level='ERROR'))

    def log(self, message: str):
        if message is None or message.strip() == "":
            raise ValueError("Message cannot be None or empty")

        self._logger.log(level=self.level, msg=message)

    def close(self):
        for handler in self._logger.handlers:
            handler.close()
            self._logger.removeHandler(handler)