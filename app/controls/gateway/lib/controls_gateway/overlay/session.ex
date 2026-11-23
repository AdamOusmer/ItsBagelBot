# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Session do
  alias ControlsGateway.SignedCookie
  alias ControlsGateway.Tts.Session, as: UserSession

  @cookie "bb_o"
  @max_age 30 * 24 * 60 * 60
  @cookie_options [
    http_only: true,
    secure: true,
    same_site: "Lax",
    path: "/o",
    max_age: @max_age
  ]

  def put(conn, overlay, token_hash) do
    claims = %{
      "overlay_id" => overlay.overlay_id,
      "generation" => overlay.generation,
      "token_hash" => token_hash,
      "user_id" => overlay.user_id,
      "exp" => SignedCookie.now() + @max_age
    }

    SignedCookie.put(conn, @cookie, claims, @cookie_options)
  end

  def read(source) do
    with {:ok, claims} <- SignedCookie.read(source, @cookie),
         %{
           "overlay_id" => id,
           "generation" => generation,
           "token_hash" => hash,
           "user_id" => user_id,
           "exp" => exp
         } <- claims,
         true <- is_binary(hash) and UserSession.valid_user_id?(user_id) do
      {:ok,
       %{overlay_id: id, generation: generation, token_hash: hash, user_id: user_id, exp: exp}}
    else
      _ -> :error
    end
  end
end
