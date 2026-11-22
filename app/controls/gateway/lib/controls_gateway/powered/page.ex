# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Page do
  alias ControlsGateway.Config
  alias ControlsGateway.Overlay.Runtime

  @path Path.expand("../../../priv/powered/page.html", __DIR__)
  @external_resource @path
  @source File.read!(@path)

  inline_hash = fn tag ->
    [[_, body]] = Regex.scan(~r/<#{tag}>(.*?)<\/#{tag}>/s, @source)
    Base.encode64(:crypto.hash(:sha256, body))
  end

  @script_hash inline_hash.("script")
  @style_hash inline_hash.("style")

  [head, tail] =
    @source
    |> String.replace("__OVERLAY_RUNTIME__", Runtime.path())
    |> then(&:binary.split(&1, "__OVERLAY_SANDBOX__"))

  @head head
  @tail tail

  @cdn "https://cdn.itsbagelbot.com"
  @dev_ancestors " http://localhost:* http://127.0.0.1:*"

  @headers %{
    "content-type" => "text/html; charset=utf-8",
    "cache-control" => "public, max-age=300",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }

  def reply(req) do
    origin = Config.sandbox_origin()
    headers = Map.put(@headers, "content-security-policy", policy(origin))
    :cowboy_req.reply(200, headers, [@head, origin, @tail], req)
  end

  defp policy(origin) do
    "default-src 'none'; script-src 'self' 'sha256-#{@script_hash}'; " <>
      "style-src 'sha256-#{@style_hash}'; " <>
      "img-src 'self' #{@cdn} https://static-cdn.jtvnw.net; media-src #{@cdn}; " <>
      "font-src 'self' #{@cdn}; connect-src 'self' #{@cdn}; frame-src #{origin}; " <>
      "base-uri 'none'; form-action 'none'; frame-ancestors https://dashboard.itsbagelbot.com" <>
      dev_ancestors()
  end

  defp dev_ancestors do
    if Application.get_env(:controls_gateway, :powered_dev_frame_ancestors, false),
      do: @dev_ancestors,
      else: ""
  end
end
