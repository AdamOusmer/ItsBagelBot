# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule PlatformWebSocket.MixProject do
  use Mix.Project

  def project do
    [app: :platform_websocket, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
  end

  def application, do: [extra_applications: [:logger, :ssl]]

  defp deps do
    [
      {:mint_web_socket, "~> 1.0"}
    ]
  end
end
