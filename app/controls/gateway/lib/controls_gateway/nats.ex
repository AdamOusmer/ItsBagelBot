# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Nats do
  @moduledoc """
  Thin Gnat wrapper for the single core-NATS connection. No JetStream, no
  streams: core pub/sub only, so a connection drop just means resubscribing.
  """
  alias ControlsGateway.{Metrics, Subjects}

  @connection :gnat

  @spec publish(String.t(), map()) :: :ok | {:error, term()}
  def publish(subject, payload) do
    case Jason.encode(payload) do
      {:ok, json} -> publish_raw(subject, json)
      {:error, reason} -> {:error, {:encode, reason}}
    end
  end

  defp publish_raw(subject, json) do
    case Process.whereis(@connection) do
      nil ->
        Metrics.count("nats_publish_dropped")
        {:error, :not_connected}

      pid ->
        Gnat.pub(pid, subject, json)
    end
  catch
    :exit, reason ->
      Metrics.count("nats_publish_dropped")
      {:error, {:exit, reason}}
  end

  @doc """
  Subscribes the caller to a device's cmd/takeover/revoked subjects on the
  current connection, all or nothing. The returned subscription remembers the
  connection it belongs to: Gnat hands out small sequential sids per
  connection process, so a stale sid is only valid on its own connection.
  """
  @spec subscribe_device(String.t()) :: {:ok, map()} | {:error, :not_connected}
  def subscribe_device(device_id) do
    case Process.whereis(@connection) do
      nil -> {:error, :not_connected}
      pid -> do_subscribe_device(pid, device_id)
    end
  end

  defp do_subscribe_device(pid, device_id) do
    subjects = [
      cmd: Subjects.device_cmd(device_id),
      takeover: Subjects.device_takeover(device_id),
      revoked: Subjects.revoked(device_id)
    ]

    case subscribe_all(pid, subjects, %{}) do
      {:ok, sids} ->
        {:ok, %{gnat: pid, sids: sids}}

      {:error, sids} ->
        unsubscribe_sids(pid, sids)
        {:error, :not_connected}
    end
  end

  defp subscribe_all(_pid, [], sids), do: {:ok, sids}

  defp subscribe_all(pid, [{key, subject} | rest], sids) do
    case subscribe(pid, subject) do
      {:ok, sid} -> subscribe_all(pid, rest, Map.put(sids, key, sid))
      _failure -> {:error, sids}
    end
  end

  defp subscribe(pid, subject) do
    Gnat.sub(pid, self(), subject)
  catch
    :exit, reason -> {:error, {:exit, reason}}
  end

  @spec unsubscribe_device(map() | nil) :: :ok
  def unsubscribe_device(nil), do: :ok

  def unsubscribe_device(%{gnat: pid, sids: sids}) do
    if Process.whereis(@connection) == pid, do: unsubscribe_sids(pid, sids)
    :ok
  end

  defp unsubscribe_sids(pid, sids) do
    Enum.each(sids, fn {_key, sid} -> unsubscribe(pid, sid) end)
  end

  defp unsubscribe(pid, sid) do
    Gnat.unsub(pid, sid)
  catch
    :exit, _reason -> :ok
  end
end
