# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Ingress.RpcServerTest do
  use ExUnit.Case, async: true

  alias Ingress.RpcServer

  test "decode_field returns the guarded field, :invalid, or a decode error reply" do
    assert {:ok, true} = RpcServer.decode_field(~s({"enabled":true}), "enabled", &is_boolean/1)
    assert :invalid = RpcServer.decode_field(~s({"enabled":"yes"}), "enabled", &is_boolean/1)
    assert :invalid = RpcServer.decode_field(~s({"other":1}), "enabled", &is_boolean/1)

    assert {:error, "json decode error: " <> _} =
             RpcServer.decode_field(~s({"enabled":), "enabled", &is_boolean/1)
  end
end
