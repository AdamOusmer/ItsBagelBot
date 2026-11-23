# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Session do
  alias ControlsGateway.SignedCookie

  @cookie "bb_tts"
  @max_age 30 * 24 * 60 * 60
  @uint64_max 18_446_744_073_709_551_615
  @cookie_options [
    http_only: true,
    secure: true,
    same_site: "Lax",
    path: "/tts",
    max_age: @max_age
  ]

  defdelegate now, to: SignedCookie
  defdelegate key, to: SignedCookie

  def put(conn, user_id, generation) do
    claims = %{"user_id" => user_id, "generation" => generation, "exp" => now() + @max_age}
    SignedCookie.put(conn, @cookie, claims, @cookie_options)
  end

  def read(source) do
    with {:ok, %{"user_id" => user_id, "generation" => generation, "exp" => exp}} <-
           SignedCookie.read(source, @cookie),
         true <- valid_identity?(user_id, generation) do
      {:ok, %{user_id: user_id, generation: generation, exp: exp}}
    else
      _ -> :error
    end
  end

  def valid_identity?(user_id, generation),
    do: valid_user_id?(user_id) and valid_generation?(generation)

  def valid_user_id?(id), do: is_integer(id) and uint64?(id)

  defp valid_generation?(generation), do: is_integer(generation) and generation >= 0

  defp uint64?(id), do: id > 0 and id <= @uint64_max
end
