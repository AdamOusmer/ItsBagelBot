# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.AuthTest do
  use ExUnit.Case, async: false
  alias ControlsGateway.Auth

  defmodule Rpc do
    def token(request) do
      send(Application.fetch_env!(:controls_gateway, :auth_test_owner), {:token_request, request})
      Application.fetch_env!(:controls_gateway, :auth_test_reply)
    end

    def authorize(request), do: token(request)
    def revoke(request), do: token(request)
  end

  setup do
    keys = [:auth_rpc, :auth_token_url, :auth_test_owner, :auth_test_reply]
    previous = Map.new(keys, &{&1, Application.fetch_env(:controls_gateway, &1)})
    Application.put_env(:controls_gateway, :auth_rpc, Rpc)

    Application.put_env(
      :controls_gateway,
      :auth_token_url,
      "https://overlay.example/oauth/desktop/token"
    )

    Application.put_env(:controls_gateway, :auth_test_owner, self())

    on_exit(fn ->
      Enum.each(previous, fn
        {key, {:ok, value}} -> Application.put_env(:controls_gateway, key, value)
        {key, :error} -> Application.delete_env(:controls_gateway, key)
      end)
    end)

    :ok
  end

  defp form,
    do: "client_id=itsbagelbot-desktop&grant_type=refresh_token&refresh_token=bbr_example"

  test "refresh forwards only scoped native fields and the configured public URL" do
    Application.put_env(
      :controls_gateway,
      :auth_test_reply,
      {:ok,
       %{
         "access_token" => "v2.sealed",
         "refresh_token" => "bbr_rotated",
         "token_type" => "DPoP",
         "expires_in" => 86400,
         "private" => "not-forwarded"
       }}
    )

    assert {200, reply, nil} = Auth.exchange(form(), "signed-proof")
    assert reply["token_type"] == "DPoP"
    refute Map.has_key?(reply, "private")
    assert_receive {:token_request, request}
    assert request["proof"] == "signed-proof"
    assert request["url"] == "https://overlay.example/oauth/desktop/token"
    refute Map.has_key?(request, "cookie")
  end

  test "nonce challenge is preserved, backend outage remains retryable" do
    Application.put_env(
      :controls_gateway,
      :auth_test_reply,
      {:ok, %{"oauth_error" => "use_dpop_nonce", "dpop_nonce" => "server-nonce"}}
    )

    assert {400, %{"error" => "use_dpop_nonce"}, "server-nonce"} = Auth.exchange(form(), "proof")
    Application.put_env(:controls_gateway, :auth_test_reply, {:error, :unavailable})
    assert {503, %{"error" => "server_error"}, nil} = Auth.exchange(form(), "proof")
  end

  test "duplicates and client-selected proxy URL are rejected before RPC" do
    assert {400, _, nil} = Auth.exchange(form() <> "&refresh_token=second", "proof")
    assert {400, _, nil} = Auth.exchange(form() <> "&url=https://evil.example", "proof")
    refute_received {:token_request, _}
  end

  test "unconfigured machine endpoint fails closed" do
    Application.put_env(:controls_gateway, :auth_token_url, "")
    assert {503, %{"error" => "server_error"}, nil} = Auth.exchange(form(), "proof")
    refute_received {:token_request, _}
  end
end
