# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.AuthHttpTest do
  use ControlsGateway.IntegrationCase, async: false

  setup do
    old = Application.fetch_env(:controls_gateway, :auth_token_url)

    Application.put_env(
      :controls_gateway,
      :auth_token_url,
      "https://overlay.example/oauth/desktop/token"
    )

    on_exit(fn ->
      case old do
        {:ok, value} -> Application.put_env(:controls_gateway, :auth_token_url, value)
        :error -> Application.delete_env(:controls_gateway, :auth_token_url)
      end
    end)

    :ok
  end

  test "native token HTTP route relays nonce without browser session", %{http_port: port} do
    {:ok, sub} = Gnat.sub(:gnat, self(), "bagel.rpc.overlay.device.token")

    task =
      Task.async(fn ->
        request(
          port,
          "client_id=itsbagelbot-desktop&grant_type=refresh_token&refresh_token=bbr_native"
        )
      end)

    assert_receive {:msg, %{body: body, reply_to: reply}}, 2_000
    decoded = Jason.decode!(body)
    assert decoded["url"] == "https://overlay.example/oauth/desktop/token"
    assert decoded["proof"] == "signed-native-proof"
    refute Map.has_key?(decoded, "cookie")

    :ok =
      Gnat.pub(
        :gnat,
        reply,
        Jason.encode!(%{"oauth_error" => "use_dpop_nonce", "dpop_nonce" => "opaque-nonce"})
      )

    assert {:ok, {400, headers, payload}} = Task.await(task)
    assert List.keyfind(headers, "dpop-nonce", 0) == {"dpop-nonce", "opaque-nonce"}
    assert Jason.decode!(payload) == %{"error" => "use_dpop_nonce"}
    assert List.keyfind(headers, "cache-control", 0) == {"cache-control", "no-store"}
    refute List.keyfind(headers, "set-cookie", 0)
    :ok = Gnat.unsub(:gnat, sub)
  end

  test "oversized body rejected without RPC", %{http_port: port} do
    {:ok, sub} = Gnat.sub(:gnat, self(), "bagel.rpc.overlay.device.token")
    assert {:ok, {400, _, _}} = request(port, String.duplicate("x", 4097))
    refute_receive {:msg, _}, 50
    :ok = Gnat.unsub(:gnat, sub)
  end

  test "native self-revoke is key-proof protected and does not use dashboard cookies", %{
    http_port: port
  } do
    old = Application.fetch_env(:controls_gateway, :auth_revoke_url)

    Application.put_env(
      :controls_gateway,
      :auth_revoke_url,
      "https://overlay.example/oauth/desktop/revoke"
    )

    on_exit(fn ->
      case old do
        {:ok, value} -> Application.put_env(:controls_gateway, :auth_revoke_url, value)
        :error -> Application.delete_env(:controls_gateway, :auth_revoke_url)
      end
    end)

    {:ok, sub} = Gnat.sub(:gnat, self(), "bagel.rpc.overlay.device.revoke")
    task = Task.async(fn -> request(port, "", "/oauth/desktop/revoke", "DPoP v2.opaque") end)
    assert_receive {:msg, %{body: body, reply_to: reply}}, 2_000
    decoded = Jason.decode!(body)
    assert decoded["token"] == "v2.opaque"
    assert decoded["url"] == "https://overlay.example/oauth/desktop/revoke"

    :ok =
      Gnat.pub(
        :gnat,
        reply,
        Jason.encode!(%{"oauth_error" => "use_dpop_nonce", "dpop_nonce" => "revoke-nonce"})
      )

    assert {:ok, {400, headers, _}} = Task.await(task)

    assert List.keyfind(headers, "www-authenticate", 0) ==
             {"www-authenticate", ~s(DPoP error="use_dpop_nonce")}

    :ok = Gnat.unsub(:gnat, sub)
    assert {:ok, {503, _, body}} = request(port, "", "/oauth/desktop/revoke", "DPoP v2.opaque")
    assert Jason.decode!(body) == %{"error" => "server_error"}
  end

  defp request(port, body, path \\ "/oauth/desktop/token", authorization \\ nil) do
    {:ok, conn} = Mint.HTTP.connect(:http, "127.0.0.1", port, protocols: [:http1])

    {:ok, conn, _ref} =
      Mint.HTTP.request(
        conn,
        "POST",
        path,
        request_headers(authorization),
        body
      )

    await_response(conn, %{status: nil, headers: [], body: ""})
  end

  defp request_headers(authorization) do
    headers = [
      {"dpop", "signed-native-proof"},
      {"content-type", "application/x-www-form-urlencoded"}
    ]

    if is_binary(authorization), do: [{"authorization", authorization} | headers], else: headers
  end

  defp await_response(conn, result) do
    receive do
      {:tcp, _, _} = message ->
        {:ok, conn, responses} = Mint.HTTP.stream(conn, message)
        {result, done} = Enum.reduce(responses, {result, false}, &response_part/2)

        if done do
          Mint.HTTP.close(conn)
          {:ok, {result.status, result.headers, result.body}}
        else
          await_response(conn, result)
        end
    after
      5_000 ->
        Mint.HTTP.close(conn)
        {:error, :timeout}
    end
  end

  defp response_part({:status, _, status}, {result, done}), do: {%{result | status: status}, done}

  defp response_part({:headers, _, headers}, {result, done}),
    do: {%{result | headers: headers}, done}

  defp response_part({:data, _, data}, {result, done}),
    do: {%{result | body: result.body <> data}, done}

  defp response_part({:done, _}, {result, _}), do: {result, true}
end
