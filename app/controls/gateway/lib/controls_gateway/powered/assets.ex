# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Assets do
  alias ControlsGateway.Http

  @dir Path.expand("../../../priv/powered/assets", __DIR__)
  @types %{".webp" => "image/webp", ".woff2" => "font/woff2"}
  @listing Path.wildcard(Path.join(@dir, "*"))
  @paths Enum.filter(@listing, &Map.has_key?(@types, Path.extname(&1)))

  for path <- @paths, do: @external_resource(path)

  @files Map.new(@paths, fn path ->
           {Path.basename(path), {Map.fetch!(@types, Path.extname(path)), File.read!(path)}}
         end)

  @headers %{
    "cache-control" => "public, max-age=31536000, immutable",
    "x-content-type-options" => "nosniff"
  }

  def __mix_recompile__?, do: Path.wildcard(Path.join(@dir, "*")) != @listing

  def reply(conn, name) do
    case Map.fetch(@files, name) do
      {:ok, {type, body}} ->
        Http.reply(conn, 200, Map.put(@headers, "content-type", type), body)

      :error ->
        Http.reply(conn, 404, %{"cache-control" => "no-store"})
    end
  end
end
