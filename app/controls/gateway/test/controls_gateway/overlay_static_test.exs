# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.OverlayStaticTest do
  use ControlsGateway.IntegrationCase, async: false

  alias ControlsGateway.Overlay.Runtime
  alias ControlsGateway.TestSupport.HttpClient

  @overlay Path.expand("../../priv/overlay", __DIR__)
  @cdn "https://cdn.itsbagelbot.com"
  @sandbox_policy "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; " <>
                    "style-src 'unsafe-inline'; img-src #{@cdn}; media-src #{@cdn}; font-src #{@cdn}; " <>
                    "connect-src 'none'; form-action 'none'; base-uri 'none'"

  test "the runtime is served under its content hash and cached for a year", %{http_port: port} do
    body = File.read!(Path.join(@overlay, "runtime.js"))

    assert %{status: 200, body: ^body, headers: headers} =
             HttpClient.request(port, "GET", Runtime.path())

    assert headers["content-type"] == "text/javascript; charset=utf-8"
    assert headers["cache-control"] == "public, max-age=31536000, immutable"
    assert headers["x-content-type-options"] == "nosniff"
  end

  test "the runtime path carries the hash of its bytes" do
    body = File.read!(Path.join(@overlay, "runtime.js"))
    hash = :crypto.hash(:sha256, body) |> binary_part(0, 16) |> Base.encode16(case: :lower)
    assert Runtime.path() == "/o/runtime/" <> hash <> ".js"
  end

  test "any other runtime name is not found", %{http_port: port} do
    for path <- ["/o/runtime/stale.js", "/o/runtime/runtime.js", "/o/runtime/"] do
      assert %{status: 404} = HttpClient.request(port, "GET", path)
    end
  end

  test "the sandbox frame pins its policy and ships the frame script", %{http_port: port} do
    frame = File.read!(Path.join(@overlay, "frame.js"))

    assert %{status: 200, headers: headers, body: body} =
             HttpClient.request(port, "GET", "/sandbox/widget-1")

    assert headers["content-security-policy"] == @sandbox_policy
    assert headers["content-type"] == "text/html; charset=utf-8"
    assert headers["referrer-policy"] == "no-referrer"
    assert body =~ "<script>" <> frame <> "</script>"
    assert body =~ ~s(<meta name="bagel-parent" content="https://overlay.itsbagelbot.com">)
  end

  test "sandbox names are widget ids only", %{http_port: port} do
    for name <- ["a%20b", "a.js", String.duplicate("a", 97)] do
      assert %{status: 404} = HttpClient.request(port, "GET", "/sandbox/" <> name)
    end
  end

  test "static routes refuse other methods", %{http_port: port} do
    for path <- [Runtime.path(), "/sandbox/widget-1"] do
      assert %{status: 405, headers: %{"allow" => "GET"}} = HttpClient.request(port, "POST", path)
    end
  end

  test "the overlay page loads its runtime from itself and frames only the sandbox origin", %{
    http_port: port
  } do
    assert %{headers: %{"content-security-policy" => csp}, body: body} =
             HttpClient.request(port, "GET", "/o")

    assert csp =~ "script-src 'self' 'sha256-"
    assert csp =~ "connect-src 'self' #{@cdn}; frame-src https://widgets.itsbagelbot.com; "
    assert body =~ ~s(<meta name="overlay-runtime" content="#{Runtime.path()}">)
    assert body =~ ~s(<meta name="overlay-sandbox" content="https://widgets.itsbagelbot.com">)
  end

  test "the sandbox origin follows the configuration", %{http_port: port} do
    Application.put_env(
      :controls_gateway,
      :overlay_sandbox_origin,
      "https://widgets.example.test"
    )

    on_exit(fn -> Application.delete_env(:controls_gateway, :overlay_sandbox_origin) end)

    assert %{headers: %{"content-security-policy" => csp}, body: body} =
             HttpClient.request(port, "GET", "/o")

    assert csp =~ "frame-src https://widgets.example.test; "
    assert body =~ ~s(content="https://widgets.example.test")
  end
end
