# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.PoweredTest do
  use ControlsGateway.IntegrationCase, async: false
  import ExUnit.CaptureLog

  alias ControlsGateway.TestSupport.HttpClient

  @resolve "rpc.internal.projector.overlays.powered.resolve"
  @token Base.url_encode64(:binary.copy(<<51>>, 32), padding: false)
  @unknown Base.url_encode64(:binary.copy(<<52>>, 32), padding: false)
  @broken Base.url_encode64(:binary.copy(<<53>>, 32), padding: false)
  @config %{
    "every_minutes" => 15,
    "show_seconds" => 8,
    "directions" => ["top", "bottom", "left", "right"],
    "align" => "end"
  }
  @assets Path.expand("../../priv/powered/assets", __DIR__)
  @types %{".webp" => "image/webp", ".woff2" => "font/woff2"}
  @page_headers %{
    "content-type" => "text/html; charset=utf-8",
    "cache-control" => "public, max-age=300",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }

  setup tags do
    setting = :overlay_trust_cloudflare_ip
    original = Application.get_env(:controls_gateway, setting)
    Application.put_env(:controls_gateway, setting, true)
    on_exit(fn -> Application.put_env(:controls_gateway, setting, original) end)

    if !tags[:no_responder] do
      responder = start_supervised!({Task, &respond/0})
      {:ok, _} = Gnat.sub(:gnat_rpc, responder, @resolve)
    end

    :ok
  end

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
               "default-src 'none'; script-src 'sha256-#{inline_hash(body, "script")}'; " <>
                 "style-src 'sha256-#{inline_hash(body, "style")}'; img-src 'self'; " <>
                 "font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; " <>
                 ancestors
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
    for {method, path, allow} <- [
          {"POST", "/powered", "GET"},
          {"DELETE", "/powered/assets/logo.webp", "GET"},
          {"GET", "/powered/config", "POST"}
        ] do
      assert %{status: 405, headers: %{"allow" => ^allow}} =
               HttpClient.request(port, method, path)
    end
  end

  test "a known token returns its config and tier uncached", %{http_port: port} do
    assert %{status: 200, headers: headers, body: body} = config(port, @token, "198.51.100.1")
    assert {headers["cache-control"], headers["content-type"]} == {"no-store", "application/json"}
    assert Jason.decode!(body) == %{"config" => @config, "premium" => true}
  end

  test "unknown tokens are 404, refusals are 503 and tokens never reach the logs", %{
    http_port: port
  } do
    log =
      capture_log([level: :debug], fn ->
        assert %{status: 404, headers: %{"cache-control" => "no-store"}} =
                 config(port, @unknown, "198.51.100.2")

        assert %{status: 503} = config(port, @broken, "198.51.100.2")
      end)

    assert log =~ "Powered overlay config rejected"

    for token <- [@unknown, @broken] do
      refute log =~ token
    end
  end

  @tag no_responder: true
  test "a missing projector is 503", %{http_port: port} do
    assert %{status: 503} = config(port, @token, "198.51.100.3")
  end

  test "malformed requests are 404", %{http_port: port} do
    ip = {"cf-connecting-ip", "198.51.100.4"}
    json = {"content-type", "application/json"}
    padded = Base.url_encode64(:binary.copy(<<51>>, 32))

    for {headers, body} <- [
          {[{"content-type", "text/plain"}, ip], Jason.encode!(%{token: @token})},
          {[json, ip], "{"},
          {[json, ip], Jason.encode!(%{token: String.slice(@token, 0, 42)})},
          {[json, ip], Jason.encode!(%{token: padded})},
          {[json, ip], Jason.encode!(%{token: @token, pad: String.duplicate("a", 256)})}
        ] do
      assert %{status: 404} =
               HttpClient.request(port, "POST", "/powered/config", headers: headers, body: body)
    end
  end

  test "failed attempts are throttled per client IP", %{http_port: port} do
    for _ <- 1..10 do
      assert %{status: 404} = config(port, @unknown, "198.51.100.5")
    end

    assert %{status: 429} = config(port, @token, "198.51.100.5")
    assert %{status: 200} = config(port, @token, "198.51.100.6")
  end

  defp config(port, token, ip) do
    HttpClient.request(port, "POST", "/powered/config",
      headers: [{"content-type", "application/json"}, {"cf-connecting-ip", ip}],
      body: Jason.encode!(%{token: token})
    )
  end

  defp inline_hash(body, tag) do
    [[_, content]] = Regex.scan(~r/<#{tag}>(.*?)<\/#{tag}>/s, body)
    Base.encode64(:crypto.hash(:sha256, content))
  end

  defp respond do
    receive do
      {:msg, %{reply_to: reply, body: body}} ->
        :ok = Gnat.pub(:gnat_rpc, reply, Jason.encode!(response(Jason.decode!(body))))
        respond()
    end
  end

  defp response(%{"token" => @token}), do: %{config: @config, premium: true}
  defp response(%{"token" => @broken}), do: %{error: "projection down", code: "unavailable"}
  defp response(_params), do: %{error: "overlay not found", code: "not_found"}
end
