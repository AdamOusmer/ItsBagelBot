# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Page do
  alias ControlsGateway.{Config, Http}
  alias ControlsGateway.Overlay.Runtime

  @path Path.expand("../../../priv/powered/page.html", __DIR__)
  @external_resource @path
  @client_path Path.expand("../../../priv/overlay/client.js", __DIR__)
  @external_resource @client_path
  @client File.read!(@client_path)
  @html @path
        |> File.read!()
        |> String.replace("{{overlay_client}}", fn _ -> @client end)
        |> String.replace("__OVERLAY_RUNTIME__", Runtime.path())

  [head, tail] = :binary.split(@html, "__OVERLAY_SANDBOX__")
  @head head
  @tail tail

  inline_hash = fn tag ->
    [[_, body]] = Regex.scan(~r/<#{tag}>(.*?)<\/#{tag}>/s, @html)
    Base.encode64(:crypto.hash(:sha256, body))
  end

  @script_hash inline_hash.("script")
  @style_hash inline_hash.("style")

  @cdn "https://cdn.itsbagelbot.com"
  @policy "default-src 'none'; script-src 'sha256-#{@script_hash}'; " <>
            "style-src 'sha256-#{@style_hash}'; " <>
            "img-src 'self' #{@cdn}; media-src #{@cdn}; font-src 'self'; " <>
            "connect-src 'self'; base-uri 'none'; form-action 'none'; " <>
            "frame-ancestors "
  @dev_ancestors " http://localhost:* http://127.0.0.1:*"

  @headers %{
    "content-type" => "text/html; charset=utf-8",
    "cache-control" => "public, max-age=300",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }

  def reply(conn), do: send_page(conn, policy())

  def reply_overlay(conn), do: send_page(conn, overlay_policy())

  def frame_ancestors, do: "https://dashboard.itsbagelbot.com" <> dev_ancestors()

  defp send_page(conn, policy) do
    headers = Map.put(@headers, "content-security-policy", policy)
    Http.reply(conn, 200, headers, [@head, Config.sandbox_origin(), @tail])
  end

  defp overlay_policy do
    origin = Config.sandbox_origin()

    "default-src 'none'; script-src 'self' 'sha256-#{@script_hash}'; " <>
      "style-src 'sha256-#{@style_hash}'; " <>
      "img-src 'self' #{@cdn} https://static-cdn.jtvnw.net; media-src #{@cdn}; " <>
      "font-src 'self' #{@cdn}; connect-src 'self' #{@cdn}; frame-src #{origin}; " <>
      "base-uri 'none'; form-action 'none'; " <>
      "frame-ancestors " <> frame_ancestors()
  end

  defp policy, do: @policy <> frame_ancestors()

  defp dev_ancestors do
    if Application.get_env(:controls_gateway, :powered_dev_frame_ancestors, false),
      do: @dev_ancestors,
      else: ""
  end
end
