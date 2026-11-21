# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Nats do
  alias ControlsGateway.OverlayRequest
  alias ControlsGateway.Tts.Session

  @resolve "rpc.internal.users.voice.overlay.resolve"
  @check "rpc.internal.users.voice.overlay.check"

  def connection, do: Process.whereis(:gnat)
  def rpc_connection, do: Process.whereis(:gnat_rpc)

  def resolve(hash) do
    case request(rpc_connection(), @resolve, %{token_hash: hash}) do
      {:ok, %{"code" => "not_found"}} -> :not_found
      {:ok, %{"code" => code}} when code not in [nil, ""] -> :unavailable
      {:ok, %{"user_id" => id, "generation" => generation}} -> identity(id, generation)
      _ -> :unavailable
    end
  end

  def check(conn, %{user_id: id, generation: generation}) do
    case request(conn, @check, %{user_id: id, generation: generation}) do
      {:ok, %{"valid" => valid}} when is_boolean(valid) -> valid
      _ -> :unavailable
    end
  end

  def request(conn, subject, body) do
    case OverlayRequest.request(conn, subject, body) do
      {:ok, %{"error" => error}} -> {:error, error}
      result -> result
    end
  end

  def subscribe(conn, subject) do
    Gnat.sub(conn, self(), subject)
  catch
    :exit, _ -> {:error, :not_connected}
  end

  defp identity(id, generation) do
    if Session.valid_identity?(id, generation), do: {:ok, id, generation}, else: :unavailable
  end
end
