# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Page do
  @path Path.expand("../../../priv/powered/page.html", __DIR__)
  @external_resource @path
  @html File.read!(@path)

  inline_hash = fn tag ->
    [[_, body]] = Regex.scan(~r/<#{tag}>(.*?)<\/#{tag}>/s, @html)
    Base.encode64(:crypto.hash(:sha256, body))
  end

  @policy "default-src 'none'; script-src 'sha256-#{inline_hash.("script")}'; " <>
            "style-src 'sha256-#{inline_hash.("style")}'; img-src 'self'; font-src 'self'; " <>
            "connect-src 'self'; base-uri 'none'; form-action 'none'; " <>
            "frame-ancestors https://dashboard.itsbagelbot.com"
  @dev_ancestors " http://localhost:* http://127.0.0.1:*"

  @headers %{
    "content-type" => "text/html; charset=utf-8",
    "cache-control" => "public, max-age=300",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }

  def reply(req) do
    headers = Map.put(@headers, "content-security-policy", policy())
    :cowboy_req.reply(200, headers, @html, req)
  end

  defp policy do
    if Application.get_env(:controls_gateway, :powered_dev_frame_ancestors, false),
      do: @policy <> @dev_ancestors,
      else: @policy
  end
end
