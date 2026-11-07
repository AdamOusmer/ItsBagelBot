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
  current connection. Returns subscription ids for unsubscribe_all/1 and the
  connection pid so the caller can monitor it for reconnects.
  """
  @spec subscribe_device(String.t()) :: {:ok, map(), pid()} | {:error, :not_connected}
  def subscribe_device(device_id) do
    case Process.whereis(@connection) do
      nil -> {:error, :not_connected}
      pid -> do_subscribe_device(pid, device_id)
    end
  catch
    :exit, _reason -> {:error, :not_connected}
  end

  defp do_subscribe_device(pid, device_id) do
    {:ok, cmd} = Gnat.sub(pid, self(), Subjects.device_cmd(device_id))
    {:ok, takeover} = Gnat.sub(pid, self(), Subjects.device_takeover(device_id))
    {:ok, revoked} = Gnat.sub(pid, self(), Subjects.revoked(device_id))
    {:ok, %{cmd: cmd, takeover: takeover, revoked: revoked}, pid}
  end

  @spec unsubscribe_device(map() | nil) :: :ok
  def unsubscribe_device(nil), do: :ok

  def unsubscribe_device(subs) do
    case Process.whereis(@connection) do
      nil -> :ok
      pid -> Enum.each(subs, fn {_key, sid} -> Gnat.unsub(pid, sid) end)
    end

    :ok
  catch
    :exit, _reason -> :ok
  end
end
