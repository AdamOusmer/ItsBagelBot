# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Session do
  alias ControlsGateway.SignedCookie

  @cookie "bb_o"
  @max_age 30 * 24 * 60 * 60
  @cookie_options %{
    http_only: true,
    secure: true,
    same_site: :lax,
    path: "/o",
    max_age: @max_age
  }

  def put(req, overlay, token_hash) do
    claims = %{
      "overlay_id" => overlay.overlay_id,
      "generation" => overlay.generation,
      "token_hash" => token_hash,
      "exp" => SignedCookie.now() + @max_age
    }

    SignedCookie.put(req, @cookie, claims, @cookie_options)
  end

  def read(req) do
    with {:ok, claims} <- SignedCookie.read(req, @cookie),
         %{"overlay_id" => id, "generation" => generation, "token_hash" => hash, "exp" => exp} <-
           claims,
         true <- is_binary(hash) do
      {:ok, %{overlay_id: id, generation: generation, token_hash: hash, exp: exp}}
    else
      _ -> :error
    end
  end
end
