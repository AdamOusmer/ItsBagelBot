# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.ObjectStoreTest do
  use ExUnit.Case, async: false

  alias Platform.Nats.ObjectStore
  alias Platform.Nats.TestSupport.{Jetstream, NatsServer}

  @moduletag :tmp_dir
  @chunks ["alpha-", "beta-", "gamma"]

  setup %{tmp_dir: dir} do
    NatsServer.start_planes(dir)
    :ok = Jetstream.create_stream!("OBJ_media", ["$O.media.C.>", "$O.media.M.>"])
  end

  defp put(key, chunks, overrides \\ %{}) do
    nuid = "nuid" <> Integer.to_string(:erlang.unique_integer([:positive]))
    for chunk <- chunks, do: Jetstream.publish!("$O.media.C." <> nuid, chunk)
    whole = IO.iodata_to_binary(chunks)

    meta =
      Map.merge(
        %{
          name: key,
          bucket: "media",
          nuid: nuid,
          size: byte_size(whole),
          chunks: length(chunks),
          digest: "SHA-256=" <> Base.url_encode64(:crypto.hash(:sha256, whole))
        },
        overrides
      )

    Jetstream.publish!("$O.media.M." <> Base.url_encode64(key), Jason.encode!(meta))
  end

  defp collect(info) do
    ObjectStore.stream(info, [], fn data, acc -> {:ok, [data | acc]} end)
  end

  test "stream yields every chunk in order when size and digest match" do
    put("user/clip", @chunks)

    {:ok, info} = ObjectStore.info(:jetstream, "media", "user/clip", timeout: 2_000)
    assert %{size: 16, chunks: 3, timeout: 2_000} = info
    assert {:ok, received} = collect(info)
    assert Enum.reverse(received) == @chunks
  end

  test "info reports a missing key" do
    assert {:error, :not_found} = ObjectStore.info(:jetstream, "media", "absent")
  end

  @bad_objects [
    {"digest", %{digest: "SHA-256=" <> Base.url_encode64(:crypto.hash(:sha256, "x"))},
     :digest_mismatch},
    {"short", %{size: 99}, :size_mismatch},
    {"long", %{size: 3}, :size_mismatch}
  ]

  test "stream fails when size or digest disagree with the metadata" do
    for {key, overrides, reason} <- @bad_objects do
      put(key, @chunks, overrides)
      assert {:ok, info} = ObjectStore.info(:jetstream, "media", key)
      assert {:error, ^reason, _acc} = collect(info), key
    end
  end

  test "stream stops at the first chunk error from the callback" do
    put("stop", @chunks)
    {:ok, info} = ObjectStore.info(:jetstream, "media", "stop")

    assert {:error, :closed, 7} =
             ObjectStore.stream(info, 0, fn _data, _acc -> {:error, :closed, 7} end)
  end

  test "a corrupt final chunk never reaches the callback" do
    put("corrupt", @chunks, %{digest: "SHA-256=" <> Base.url_encode64(:crypto.hash(:sha256, "x"))})

    {:ok, info} = ObjectStore.info(:jetstream, "media", "corrupt")

    test = self()

    result =
      ObjectStore.stream(info, 0, fn data, acc ->
        send(test, {:chunk, data})
        {:ok, acc}
      end)

    assert result == {:error, :digest_mismatch, 0}
    assert_received {:chunk, "alpha-"}
    assert_received {:chunk, "beta-"}
    refute_received {:chunk, "gamma"}
  end
end
