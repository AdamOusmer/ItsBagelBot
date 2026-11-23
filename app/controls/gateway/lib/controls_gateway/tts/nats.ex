# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Nats do
  require Logger

  alias ControlsGateway.OverlayRequest
  alias ControlsGateway.Tts.Session
  alias Platform.Cache
  alias Platform.Nats.Planes

  @resolve "rpc.internal.projector.overlays.tts.resolve"
  @check "rpc.internal.projector.overlays.tts.check"

  def resolve(hash) do
    case Cache.get_or_load(:tts_resolve, hash, fn -> fetch_identity(hash) end) do
      {:ok, :not_found} -> {:not_found, :unknown_token}
      {:ok, {id, generation}} -> {:ok, id, generation}
      {:error, reason} -> {:unavailable, reason}
    end
  end

  def check(%{user_id: id, generation: generation} = session, rpc \\ :rpc) do
    case Cache.get_or_load(:tts_check, {id, generation}, fn -> fetch_check(session, rpc) end) do
      {:ok, valid} ->
        valid

      {:error, reason} ->
        Logger.warning("TTS check unavailable", user_id: id, reason: reason)
        :unavailable
    end
  end

  defp fetch_identity(hash) do
    case OverlayRequest.request(&Planes.request/1, @resolve, %{token_hash: hash}) do
      {:ok, %{"code" => "not_found"}} -> {:ok, :not_found}
      {:ok, %{"code" => code} = reply} when code not in [nil, ""] -> unavailable(reply)
      {:ok, %{"user_id" => id, "generation" => generation}} -> identity(id, generation)
      other -> unavailable(other)
    end
  end

  defp fetch_check(%{user_id: id, generation: generation}, rpc) do
    requester = &Planes.request(&1, rpc)

    case request(requester, @check, %{user_id: id, generation: generation}) do
      {:ok, %{"valid" => valid}} when is_boolean(valid) -> {:ok, valid}
      other -> unavailable(other)
    end
  end

  defp request(requester, subject, body) do
    case OverlayRequest.request(requester, subject, body) do
      {:ok, %{"error" => error}} -> {:error, error}
      result -> result
    end
  end

  defp unavailable(reason), do: {:error, inspect(reason, limit: 5)}

  defp identity(id, generation) do
    if Session.valid_identity?(id, generation),
      do: {:ok, {id, generation}},
      else: {:error, :invalid_identity}
  end
end
