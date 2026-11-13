# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardSessionTest do
  use ExUnit.Case, async: true

  alias Ingress.ShardSession

  test "reconnect stagger puts each of ten shards in its own second" do
    slots = for id <- 0..9, do: div(ShardSession.stagger_ms(id), 1_000)

    assert slots == Enum.to_list(0..9)
    assert ShardSession.stagger_ms(10) == ShardSession.stagger_ms(0)
  end

  describe "permanent_bind_error?/1" do
    test "invalid shard id is permanent" do
      errors = [
        %{
          "code" => "invalid_parameter",
          "id" => "5",
          "message" => "invalid shard id, must be a number greater or equal to 0 and less than 5"
        }
      ]

      assert ShardSession.permanent_bind_error?({:shard_errors, errors})
    end

    test "mixed errors count as permanent when any is invalid_parameter" do
      errors = [
        %{"code" => "websocket_disconnected", "id" => "3", "message" => "socket gone"},
        %{"code" => "invalid_parameter", "id" => "5", "message" => "invalid shard id"}
      ]

      assert ShardSession.permanent_bind_error?({:shard_errors, errors})
    end

    test "transient session errors are not permanent" do
      errors = [
        %{"code" => "websocket_disconnected", "id" => "2", "message" => "socket gone"},
        %{"code" => "websocket_failed_ping_pong", "id" => "2", "message" => "no pong"}
      ]

      refute ShardSession.permanent_bind_error?({:shard_errors, errors})
    end

    test "non-shard errors (transport, HTTP) are not permanent" do
      refute ShardSession.permanent_bind_error?(:timeout)
      refute ShardSession.permanent_bind_error?({:http_error, 500})
      refute ShardSession.permanent_bind_error?({:shard_errors, []})
    end
  end
end
