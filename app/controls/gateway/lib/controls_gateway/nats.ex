# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Nats do
  @moduledoc "Desktop core subjects carried on the hub connection."
  alias ControlsGateway.{Metrics, Subjects}
  alias Platform.Nats.Planes

  @spec publish(String.t(), map()) :: :ok | {:error, term()}
  def publish(subject, payload) do
    case Jason.encode(payload) do
      {:ok, json} -> publish_raw(subject, json)
      {:error, reason} -> {:error, {:encode, reason}}
    end
  end

  defp publish_raw(subject, json) do
    case Planes.publish(:jetstream, subject, json) do
      {:error, :not_connected} = error ->
        Metrics.count("nats_publish_dropped")
        error

      result ->
        result
    end
  end

  @doc """
  Subscribes the caller to a device's cmd/takeover/revoked subjects on the
  current connection, all or nothing. The returned subscription remembers the
  connection it belongs to: Gnat hands out small sequential sids per
  connection process, so a stale sid is only valid on its own connection.
  """
  @spec subscribe_device(String.t()) :: {:ok, map()} | {:error, :not_connected}
  def subscribe_device(device_id) do
    case Planes.connection(:jetstream) do
      nil -> {:error, :not_connected}
      jetstream -> do_subscribe_device(jetstream, device_id)
    end
  end

  defp do_subscribe_device(jetstream, device_id) do
    subjects = [
      cmd: Subjects.device_cmd(device_id),
      takeover: Subjects.device_takeover(device_id),
      revoked: Subjects.revoked(device_id)
    ]

    case subscribe_all(jetstream, subjects, %{}) do
      {:ok, sids} ->
        {:ok, %{jetstream: jetstream, sids: sids}}

      {:error, sids} ->
        unsubscribe_sids(jetstream, sids)
        {:error, :not_connected}
    end
  end

  defp subscribe_all(_jetstream, [], sids), do: {:ok, sids}

  defp subscribe_all(jetstream, [{key, subject} | rest], sids) do
    case Planes.subscribe(jetstream, self(), subject) do
      {:ok, sid} -> subscribe_all(jetstream, rest, Map.put(sids, key, sid))
      _failure -> {:error, sids}
    end
  end

  @spec unsubscribe_device(map() | nil) :: :ok
  def unsubscribe_device(nil), do: :ok

  def unsubscribe_device(%{jetstream: jetstream, sids: sids}) do
    unsubscribe_sids(jetstream, sids)
  end

  defp unsubscribe_sids(jetstream, sids) do
    Enum.each(sids, fn {_key, sid} -> Planes.unsubscribe(jetstream, sid) end)
  end
end
