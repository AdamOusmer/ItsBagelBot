# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Sift.Pack do
  @moduledoc false

  # Same thresholds, window and frame magic as pkg/codec Pack, so frames interoperate with Go.
  @min_size 256
  @min_savings 10
  @window_log 17
  @max_decoded 4 * 1024 * 1024
  @magic <<0x28, 0xB5, 0x2F, 0xFD>>

  def pack(src) when is_binary(src) and byte_size(src) < @min_size, do: src
  def pack(src) when is_binary(src) and byte_size(src) > @max_decoded, do: src

  def pack(src) when is_binary(src) do
    packed =
      src
      |> :zstd.compress(%{compressionLevel: 1, windowLog: @window_log, checksumFlag: false})
      |> IO.iodata_to_binary()

    if byte_size(packed) * 100 > byte_size(src) * (100 - @min_savings), do: src, else: packed
  end

  def unpack(<<@magic, _::binary>> = src) do
    with {:ok, header} <- :zstd.get_frame_header(src),
         size = Map.get(header, :frameContentSize),
         :ok <- within_limit(size),
         {:ok, out} <- decompress(src),
         true <- not is_integer(size) or byte_size(out) == size do
      {:ok, out}
    else
      {:error, :too_large} = error -> error
      _ -> {:error, :invalid_frame}
    end
  rescue
    _ -> {:error, :invalid_frame}
  end

  def unpack(src) when is_binary(src), do: {:ok, src}

  defp within_limit(size) when is_integer(size) and size > @max_decoded, do: {:error, :too_large}
  defp within_limit(_size), do: :ok

  defp decompress(src) do
    {:ok, context} = :zstd.context(:decompress, %{windowLogMax: @window_log})

    try do
      stream(context, src, [], 0)
    after
      :zstd.close(context)
    end
  end

  defp stream(context, input, acc, size) do
    case :zstd.stream(context, input) do
      {:continue, rest, output} ->
        more(context, rest, [acc | output], size + IO.iodata_length(output))

      {:continue, output} ->
        done([acc | output], size + IO.iodata_length(output))

      {:done, output} ->
        done([acc | output], size + IO.iodata_length(output))
    end
  end

  defp more(_context, _rest, _acc, size) when size > @max_decoded, do: {:error, :too_large}
  defp more(context, rest, acc, size), do: stream(context, rest, acc, size)

  defp done(_acc, size) when size > @max_decoded, do: {:error, :too_large}
  defp done(acc, _size), do: {:ok, IO.iodata_to_binary(acc)}
end
