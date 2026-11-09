# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Valkey.RetainedStreamTest do
  use ExUnit.Case
  @moduletag skip: is_nil(System.get_env("VALKEY_TEST_PORT"))
  alias Platform.Valkey.RetainedStream, as: Stream

  @tag :integration
  test "retains cursors and atomically transfers migration across connections" do
    port = System.get_env("VALKEY_TEST_PORT")

    if port do
      {:ok, a} = Redix.start_link(host: "127.0.0.1", port: String.to_integer(port))
      {:ok, b} = Redix.start_link(host: "127.0.0.1", port: String.to_integer(port))
      key = "test:{#{Base.encode16(:crypto.strong_rand_bytes(12))}}"
      assert {:ok, [""]} = Stream.call(a, key, "open", ["a", "0", ""])
      assert {:ok, ["1"]} = Stream.call(a, key, "append", ["one", "2", "20", "1"])
      assert {:ok, ["2"]} = Stream.call(b, key, "append", ["two", "2", "20", "2"])
      assert {:ok, ["2"]} = Stream.call(a, key, "append", ["two", "2", "20", "2"])
      assert {:error, "invalid"} = Stream.call(a, key, "append", ["different", "2", "20", "2"])
      assert {:error, "full"} = Stream.call(b, key, "append", ["three", "2", "20", "3"])
      assert {:ok, ["token"]} = Stream.call(a, key, "migrate", ["a", "0", "10000", "token"])
      assert {:ok, ["2"]} = Stream.call(b, key, "open", ["b", "0", "token"])
      assert {:error, "invalid"} = Stream.call(a, key, "checkpoint", ["a", "3"])
      assert {:error, "invalid"} = Stream.call(b, key, "open", ["c", "0", "token"])
      assert {:ok, ["one", "two"]} = Stream.call(b, key, "read", ["b", "0", "16"])
      assert {:ok, ["3"]} = Stream.call(a, key, "append", ["three", "3", "20", "3"])
      assert {:error, "invalid"} = Stream.call(a, key, "checkpoint", ["a", "3"])
      assert {:ok, []} = Stream.call(a, key, "read", ["a", "2", "16"])
      assert {:ok, ["three"]} = Stream.call(b, key, "read", ["b", "2", "16"])
      assert {:ok, []} = Stream.call(b, key, "close", ["b"])
      assert {:ok, ["three"]} = Stream.call(a, key, "read", ["a", "2", "16"])
      assert {:ok, []} = Stream.call(a, key, "close", ["a"])
      assert {:ok, ["4"]} = Stream.call(a, key, "append", ["four", "2", "20", "4"])
      assert {:error, "invalid"} = Stream.call(a, key, "open", ["c", "0", ""])

      large = key <> ":large"

      assert {:ok, "9007199254740992-0"} =
               Redix.command(a, ["XADD", large <> ":stream", "9007199254740992-0", "data", "seed"])

      assert {:ok, ["9007199254740993"]} =
               Stream.call(a, large, "append", ["x", "2", "20", "9007199254740993"])

      assert {:ok, ["9007199254740993"]} =
               Stream.call(a, large, "append", ["x", "2", "20", "9007199254740993"])

      high = key <> ":high"

      assert {:ok, "18446744073709551614-0"} =
               Redix.command(a, [
                 "XADD",
                 high <> ":stream",
                 "18446744073709551614-0",
                 "data",
                 "seed"
               ])

      assert {:ok, ["18446744073709551615"]} =
               Stream.call(a, high, "append", ["x", "2", "20", "18446744073709551615"])

      assert {:error, "invalid"} =
               Stream.call(a, high, "append", ["y", "2", "20", "18446744073709551616"])
    end
  end

  @tag :integration
  test "checkpoints roll retention while a slower lease remains protected" do
    if port = System.get_env("VALKEY_TEST_PORT") do
      {:ok, a} = Redix.start_link(host: "127.0.0.1", port: String.to_integer(port))
      {:ok, b} = Redix.start_link(host: "127.0.0.1", port: String.to_integer(port))
      key = "checkpoint:{#{Base.encode16(:crypto.strong_rand_bytes(12))}}"
      assert {:ok, [""]} = Stream.call(a, key, "open", ["fast", "0", ""])

      for n <- 1..100 do
        assert {:ok, [_]} = Stream.call(a, key, "append", ["x", "4", "32", Integer.to_string(n)])
        assert {:ok, []} = Stream.call(a, key, "checkpoint", ["fast", Integer.to_string(n)])
      end

      assert {:error, "invalid"} = Stream.call(a, key, "checkpoint", ["fast", "99"])
      assert {:error, "invalid"} = Stream.call(a, key, "checkpoint", ["fast", "101"])
      assert {:ok, [""]} = Stream.call(b, key, "open", ["slow", "99", ""])
      assert {:ok, ["101"]} = Stream.call(a, key, "append", ["x", "4", "32", "101"])
      assert {:ok, ["102"]} = Stream.call(a, key, "append", ["x", "4", "32", "102"])
      assert {:ok, ["103"]} = Stream.call(a, key, "append", ["x", "4", "32", "103"])
      assert {:error, "full"} = Stream.call(a, key, "append", ["x", "4", "32", "104"])
      assert {:ok, []} = Stream.call(b, key, "checkpoint", ["slow", "100"])
      assert {:ok, ["104"]} = Stream.call(a, key, "append", ["x", "4", "32", "104"])
    end
  end

  @tag :integration
  test "stale accounting rebuilds from records and failed append leaves head unchanged" do
    port = String.to_integer(System.fetch_env!("VALKEY_TEST_PORT"))
    {:ok, conn} = Redix.start_link(host: "127.0.0.1", port: port)
    key = "repair:{#{Base.encode16(:crypto.strong_rand_bytes(12))}}"
    assert {:ok, [""]} = Stream.call(conn, key, "open", ["lease", "0", ""])
    assert {:ok, ["1"]} = Stream.call(conn, key, "append", ["1234", "3", "8", "1"])
    assert {:ok, ["1", "4"]} = Redix.command(conn, ["HMGET", key <> ":bytes", "head", "size"])
    assert {:ok, _} = Redix.command(conn, ["HSET", key <> ":bytes", "head", "0", "size", "0"])
    assert {:ok, ["2"]} = Stream.call(conn, key, "append", ["5678", "3", "8", "2"])
    assert {:ok, ["2", "8"]} = Redix.command(conn, ["HMGET", key <> ":bytes", "head", "size"])
    assert {:error, "full"} = Stream.call(conn, key, "append", ["9", "3", "8", "3"])
    assert {:ok, ["2", "8"]} = Redix.command(conn, ["HMGET", key <> ":bytes", "head", "size"])
    assert {:ok, ["2"]} = Stream.call(conn, key, "append", ["5678", "3", "8", "2"])
    assert {:ok, []} = Stream.call(conn, key, "checkpoint", ["lease", "1"])
    assert {:ok, ["3"]} = Stream.call(conn, key, "append", ["9", "3", "8", "3"])
  end
end
