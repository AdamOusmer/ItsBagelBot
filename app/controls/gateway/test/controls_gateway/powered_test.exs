# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.PoweredTest do
  use ControlsGateway.IntegrationCase, async: false

  alias ControlsGateway.TestSupport.HttpClient

  @assets Path.expand("../../priv/powered/assets", __DIR__)
  @types %{".webp" => "image/webp", ".woff2" => "font/woff2"}
  @page_headers %{
    "content-type" => "text/html; charset=utf-8",
    "cache-control" => "public, max-age=300",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }

  test "the page sends a policy that matches its inline style and script", %{http_port: port} do
    dashboard = "frame-ancestors https://dashboard.itsbagelbot.com"

    for {dev, ancestors} <- [
          {false, dashboard},
          {true, dashboard <> " http://localhost:* http://127.0.0.1:*"}
        ] do
      Application.put_env(:controls_gateway, :powered_dev_frame_ancestors, dev)
      on_exit(fn -> Application.delete_env(:controls_gateway, :powered_dev_frame_ancestors) end)

      assert %{status: 200, headers: headers, body: body} =
               HttpClient.request(port, "GET", "/powered")

      assert Map.take(headers, Map.keys(@page_headers)) == @page_headers

      assert headers["content-security-policy"] ==
               "default-src 'none'; script-src 'self' 'sha256-#{inline_hash(body, "script")}'; " <>
                 "style-src 'sha256-#{inline_hash(body, "style")}'; " <>
                 "img-src 'self' https://cdn.itsbagelbot.com https://static-cdn.jtvnw.net; " <>
                 "media-src https://cdn.itsbagelbot.com; font-src 'self' https://cdn.itsbagelbot.com; " <>
                 "connect-src 'self' https://cdn.itsbagelbot.com; " <>
                 "frame-src https://widgets.itsbagelbot.com; " <>
                 "base-uri 'none'; form-action 'none'; " <> ancestors
    end
  end

  test "only compiled assets are served and they are immutable", %{http_port: port} do
    for path <- Path.wildcard(Path.join(@assets, "*")) do
      body = File.read!(path)
      type = Map.fetch!(@types, Path.extname(path))

      assert %{status: 200, body: ^body, headers: %{"content-type" => ^type} = headers} =
               HttpClient.request(port, "GET", "/powered/assets/" <> Path.basename(path))

      assert headers["cache-control"] == "public, max-age=31536000, immutable"
    end

    for name <- ["missing.webp", "page.html", "..%2Fpage.html"] do
      assert %{status: 404} = HttpClient.request(port, "GET", "/powered/assets/" <> name)
    end
  end

  test "wrong methods are refused with the allowed method", %{http_port: port} do
    for {method, path} <- [{"POST", "/powered"}, {"DELETE", "/powered/assets/logo.webp"}] do
      assert %{status: 405, headers: %{"allow" => "GET"}} = HttpClient.request(port, method, path)
    end
  end

  defp inline_hash(body, tag) do
    [[_, content]] = Regex.scan(~r/<#{tag}>(.*?)<\/#{tag}>/s, body)
    Base.encode64(:crypto.hash(:sha256, content))
  end
end
