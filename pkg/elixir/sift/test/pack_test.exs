defmodule Platform.Sift.PackTest do
  use ExUnit.Case, async: true

  alias Platform.Sift

  @magic <<0x28, 0xB5, 0x2F, 0xFD>>

  test "small, incompressible and oversized inputs are returned unchanged" do
    small = String.duplicate("a", 255)
    assert Sift.pack(small) == small
    noise = :crypto.strong_rand_bytes(4_096)
    assert Sift.pack(noise) == noise
    big = String.duplicate("a", 4 * 1024 * 1024 + 1)
    assert Sift.pack(big) == big
  end

  test "compressible input round-trips through a zstd frame" do
    json = Sift.encode!(%{"rows" => List.duplicate(%{"user" => "abc", "points" => 10}, 200)})
    packed = Sift.pack(json)
    assert <<@magic, _::binary>> = packed
    assert byte_size(packed) < byte_size(json)
    assert {:ok, ^json} = Sift.unpack(packed)
  end

  test "unpack passes plain input through and caps decoded size at 4 MiB" do
    assert {:ok, "plain"} = Sift.unpack("plain")

    bomb =
      :zstd.compress(:binary.copy("a", 5 * 1024 * 1024), %{windowLog: 17})
      |> IO.iodata_to_binary()

    assert {:error, :too_large} = Sift.unpack(bomb)
    assert {:error, :invalid_frame} = Sift.unpack(@magic <> "garbage")

    json = Sift.encode!(List.duplicate(%{"k" => "value"}, 100))
    packed = Sift.pack(json)
    truncated = binary_part(packed, 0, byte_size(packed) - 4)
    assert {:error, :invalid_frame} = Sift.unpack(truncated)
  end
end
