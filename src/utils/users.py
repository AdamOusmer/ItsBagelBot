"""
Use the databse to recover all users of the bot.
"""


from database.models.BotUsers import BotUsers

from utils import settings

class Users:

    @staticmethod
    def get_all_users():
        """Retrieve all bot users' usernames from the database, skipping the bot account."""
        return [row[0] for row in BotUsers.select(BotUsers.username).tuples() if row[0] != settings.BOT_LOGIN]

    @staticmethod
    def get_user_by_username(username: str):
        """Retrieve a bot user by username."""
        return BotUsers.get_user(username)

    @staticmethod
    def get_user_by_id(user_id: str):
        """Retrieve a bot user by user ID."""
        return BotUsers.get_user_by_id(user_id)

    @staticmethod
    def add_or_update_user(username: str, display_name: str, user_id: str):
        """Add a new bot user or update an existing one."""
        user = BotUsers.get_user(username)
        if user:
            user.username = username
            user.display_name = display_name
            user.user_id = user_id
            user.save()
        else:
            BotUsers.create(
                username=username,
                display_name=display_name,
                user_id=user_id
            )


    @staticmethod
    def remove_user(username: str):
        """Remove a bot user by username."""
        user = BotUsers.get_user(username)
        if user:
            user.delete_instance()

    @staticmethod
    def check_existence(username: str) -> bool:
        """Check if a bot user exists by username."""
        user = BotUsers.get_user(username)
        return user is not None

