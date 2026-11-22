# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Nats do
  require Logger

  alias ControlsGateway.OverlayRequest
  alias ControlsGateway.Tts.Session
  alias Platform.Nats.Planes

  @resolve "rpc.internal.projector.overlays.tts.resolve"
  @check "rpc.internal.projector.overlays.tts.check"

  def resolve(hash) do
    case OverlayRequest.request(&Planes.request/1, @resolve, %{token_hash: hash}) do
      {:ok, %{"code" => "not_found"}} -> {:not_found, :unknown_token}
      {:ok, %{"code" => code} = reply} when code not in [nil, ""] -> unavailable(reply)
      {:ok, %{"user_id" => id, "generation" => generation}} -> identity(id, generation)
      other -> unavailable(other)
    end
  end

  def check(%{user_id: id, generation: generation}, rpc \\ :rpc) do
    case request(&Planes.request(&1, rpc), @check, %{user_id: id, generation: generation}) do
      {:ok, %{"valid" => valid}} when is_boolean(valid) ->
        valid

      other ->
        Logger.warning("TTS check unavailable", user_id: id, reason: inspect(other, limit: 5))
        :unavailable
    end
  end

  def jetstream_request(jetstream, subject, body) do
    request(&Planes.jetstream_request(&1, jetstream), subject, body)
  end

  defp request(requester, subject, body) do
    case OverlayRequest.request(requester, subject, body) do
      {:ok, %{"error" => error}} -> {:error, error}
      result -> result
    end
  end

  defp unavailable(reason), do: {:unavailable, inspect(reason, limit: 5)}

  defp identity(id, generation) do
    if Session.valid_identity?(id, generation),
      do: {:ok, id, generation},
      else: {:unavailable, :invalid_identity}
  end
end
