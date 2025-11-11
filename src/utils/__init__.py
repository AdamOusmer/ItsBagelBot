from utils.logger import Logger
from utils.bus import Bus

EventBUS = Bus(logger_name="System.EventBUS")
SystemBUS = Bus(logger_name="System.SystemBUS")

from utils.settings import Settings


settings = Settings()
