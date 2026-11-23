# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Page do
  alias ControlsGateway.Http

  @path Path.expand("../../../priv/powered/page.html", __DIR__)
  @external_resource @path
  @client_path Path.expand("../../../priv/overlay/client.js", __DIR__)
  @external_resource @client_path
  @client File.read!(@client_path)
  @html @path |> File.read!() |> String.replace("{{overlay_client}}", fn _ -> @client end)

  inline_hash = fn tag ->
    [[_, body]] = Regex.scan(~r/<#{tag}>(.*?)<\/#{tag}>/s, @html)
    Base.encode64(:crypto.hash(:sha256, body))
  end

  @policy "default-src 'none'; script-src 'sha256-#{inline_hash.("script")}'; " <>
            "style-src 'sha256-#{inline_hash.("style")}'; " <>
            "img-src 'self' https://cdn.itsbagelbot.com; media-src https://cdn.itsbagelbot.com; " <>
            "font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; " <>
            "frame-ancestors https://dashboard.itsbagelbot.com"
  @dev_ancestors " http://localhost:* http://127.0.0.1:*"

  @headers %{
    "content-type" => "text/html; charset=utf-8",
    "cache-control" => "public, max-age=300",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }

  def reply(conn) do
    headers = Map.put(@headers, "content-security-policy", policy())
    Http.reply(conn, 200, headers, @html)
  end

  defp policy do
    if Application.get_env(:controls_gateway, :powered_dev_frame_ancestors, false),
      do: @policy <> @dev_ancestors,
      else: @policy
  end
end
