"""
Abstraction layer from the application to the Twitch Endpoints for authentication and user info.
Abstraction layer from the application to the database models for storing tokens and user info.
"""

import threading
import webbrowser
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

import time
from datetime import datetime, UTC, timedelta

from utils import settings

# Twitch endpoint imports
from API.twitchEndPoints.userInfos import get_user_infos
from API.twitchEndPoints.userToken import get_user_token
from API.twitchEndPoints.refreshToken import refresh_user_token
from API.twitchEndPoints.appToken import get_app_token

# Database models
from database.models import APITokens as Tokens
from database.models import BotUsers as Users


class TwitchAuthHelper:
    """
    Handles Twitch authentication and user info management.
    All user lookups now rely on the username (not user_id).
    """

    # -------------------------------
    # User and Token Retrieval Helpers
    # -------------------------------

    def get_or_create_user_info(self, username: str, access_token: str = None):
        """Retrieve or create user info from Twitch and store in DB."""
        if not username or username.strip() == "":
            raise ValueError("Username must be provided.")

        username = username.lower()

        user = Users.get_user(username)

        if user is None:
            # Fetch from Twitch API if not in DB
            user_data = get_user_infos(username, access_token)
            user = Users.create(
                username=user_data["login"].lower(),
                display_name=user_data["display_name"],
                user_id=user_data["id"],
            )
            print(f"🆕 Created new user record for {username}")
        else:
            # Update if incomplete or outdated
            if not user.user_id or not user.display_name:
                user_data = get_user_infos(username, access_token)
                user.display_name = user_data["display_name"]
                user.user_id = user_data["id"]
                user.save()
                print(f"🔁 Updated user info for {username}")

        return user

    def retrive_token(self, name: str, username: str):
        """Retrieve a stored token for a given username."""
        username = username.lower()
        return Tokens.get_token(name, username)

    # -------------------------------
    # OAuth and Token Management
    # -------------------------------

    def request_oauth_token(self, username: str, isBot: bool = False) -> Tokens:
        """Authorize via browser and store new user OAuth token."""
        username = username.lower()

        code = self.browser_authorize(isBot)
        token_data = get_user_token(code)

        token = Tokens.create(
            name="twitch_user_oauth",
            streamer_name=username,
            access_token=token_data["access_token"],
            refresh_token=token_data.get("refresh_token"),
            expires_at=datetime.now(UTC) + timedelta(seconds=token_data["expires_in"]),
        )

        print(f"✅ Stored new OAuth token for {username}")
        return token

    def refresh_oauth_token(self, oauth_token: Tokens) -> Tokens:
        """Refresh user OAuth token using refresh_token."""
        if oauth_token is None or not getattr(oauth_token, "refresh_token", None):
            raise ValueError("A valid token object with a refresh_token is required.")

        data = refresh_user_token(oauth_token.refresh_token)
        return Tokens.update_from_response(oauth_token, data)

    def request_app_token(self) -> Tokens:
        """Request new app access token."""
        data = get_app_token()
        token = Tokens.create(
            name="twitch",
            streamer_name=settings.BOT_LOGIN.lower(),
            access_token=data["access_token"],
            refresh_token=None,
            expires_at=datetime.now(UTC) + timedelta(seconds=data["expires_in"]),
        )
        print("✅ Created new app token.")
        return token

    # -------------------------------
    # Browser Authorization Flow
    # -------------------------------

    def browser_authorize(self, isBot: bool = False) -> str:
        """Opens Twitch OAuth in browser and captures auth code."""
        scopes = (
            "chat:read+chat:edit+moderator:read:followers+moderator:read:chatters+"
            "user:bot+user:read:chat+user:write:chat"
        )
        if isBot:
            scopes += "+channel:bot"

        auth_url = (
            f"https://id.twitch.tv/oauth2/authorize"
            f"?client_id={settings.TWITCH['CLIENT_ID']}"
            f"&redirect_uri={settings.TWITCH['REDIRECT_URI']}"
            f"&response_type=code"
            f"&scope={scopes}"
        )

        try:
            if isBot:
                firefox_dev = webbrowser.get('open -a "/Applications/Firefox Developer Edition.app" %s')
                firefox_dev.open(auth_url)
            else:
                webbrowser.open(auth_url)
        except webbrowser.Error:
            webbrowser.open(auth_url)

        code_holder = {}

        class OAuthHandler(BaseHTTPRequestHandler):
            def do_GET(self):
                parsed = urlparse(self.path)
                query = parse_qs(parsed.query)
                if "code" in query:
                    code_holder["code"] = query["code"][0]
                    self.send_response(200)
                    self.send_header("Content-type", "text/html")
                    self.end_headers()
                    message = "<html><body><h2>Twitch authorization successful. You can close this window.</h2></body></html>"
                    self.wfile.write(message.encode("utf-8"))
                    threading.Thread(target=self.server.shutdown).start()

        parsed = urlparse(settings.TWITCH["REDIRECT_URI"])
        host = parsed.hostname or "localhost"
        port = parsed.port or 80

        server = HTTPServer((host, port), OAuthHandler)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()

        while "code" not in code_holder:
            time.sleep(0.1)

        thread.join()
        return code_holder["code"]