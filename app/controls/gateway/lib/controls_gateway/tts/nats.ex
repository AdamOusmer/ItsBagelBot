# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Nats do
  alias ControlsGateway.OverlayRequest
  alias ControlsGateway.Tts.Session
  alias Platform.Nats.Planes

  @resolve "rpc.internal.users.voice.overlay.resolve"
  @check "rpc.internal.users.voice.overlay.check"

  def resolve(hash) do
    case request(&Planes.request/1, @resolve, %{token_hash: hash}) do
      {:ok, %{"code" => "not_found"}} -> :not_found
      {:ok, %{"code" => code}} when code not in [nil, ""] -> :unavailable
      {:ok, %{"user_id" => id, "generation" => generation}} -> identity(id, generation)
      _ -> :unavailable
    end
  end

  def check(%{user_id: id, generation: generation}, rpc \\ :rpc) do
    case request(&Planes.request(&1, rpc), @check, %{user_id: id, generation: generation}) do
      {:ok, %{"valid" => valid}} when is_boolean(valid) -> valid
      _ -> :unavailable
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

  defp identity(id, generation) do
    if Session.valid_identity?(id, generation), do: {:ok, id, generation}, else: :unavailable
  end
end
