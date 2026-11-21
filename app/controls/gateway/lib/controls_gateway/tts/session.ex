# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Session do
  @cookie "bb_tts"
  @max_age 30 * 24 * 60 * 60
  @uint64_max 18_446_744_073_709_551_615
  @cookie_options %{
    http_only: true,
    secure: true,
    same_site: :lax,
    path: "/tts",
    max_age: @max_age
  }

  def put(req, user_id, generation) do
    claims = %{"user_id" => user_id, "generation" => generation, "exp" => now() + @max_age}
    payload = claims |> Jason.encode!() |> Base.url_encode64(padding: false)
    signature = sign(payload) |> Base.url_encode64(padding: false)
    :cowboy_req.set_resp_cookie(@cookie, payload <> "." <> signature, req, @cookie_options)
  end

  def read(req) do
    with key <- key(),
         true <- usable_key?(key),
         {_, cookie} <- List.keyfind(:cowboy_req.parse_cookies(req), @cookie, 0),
         [payload, signature] <- :binary.split(cookie, "."),
         {:ok, mac} when byte_size(mac) == 32 <- Base.url_decode64(signature, padding: false),
         true <- :crypto.hash_equals(mac, sign(payload)),
         {:ok, json} <- Base.url_decode64(payload, padding: false),
         {:ok, %{"user_id" => user_id, "generation" => generation, "exp" => exp}} <-
           Jason.decode(json),
         true <- valid_identity?(user_id, generation),
         true <- unexpired?(exp) do
      {:ok, %{user_id: user_id, generation: generation, exp: exp}}
    else
      _ -> :error
    end
  catch
    :exit, {:request_error, _, _} -> :error
  end

  defp usable_key?(key) when is_binary(key), do: byte_size(key) >= 32
  defp usable_key?(_key), do: false

  defp unexpired?(exp) when is_integer(exp), do: exp > now()
  defp unexpired?(_exp), do: false

  def valid_identity?(user_id, generation) do
    valid_user_id?(user_id) and is_integer(generation) and generation >= 0
  end

  defp valid_user_id?(id), do: is_integer(id) and id > 0 and id <= @uint64_max

  defp sign(payload), do: :crypto.mac(:hmac, :sha256, key(), payload)

  def key, do: Application.get_env(:controls_gateway, :overlay_cookie_key)

  def now, do: System.system_time(:second)
end
