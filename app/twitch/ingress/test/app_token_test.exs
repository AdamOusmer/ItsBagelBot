# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Twitch.AppTokenTest do
  use ExUnit.Case, async: false

  alias Ingress.Twitch.{Api, AppToken}

  test "a conduit reconcile before the token process starts returns an error instead of exiting" do
    refute Process.whereis(AppToken)
    assert {:error, {:app_token_unavailable, {:noproc, _}}} = AppToken.get()
    assert {:error, {:app_token_unavailable, _}} = Api.ensure_conduit()
  end
end
