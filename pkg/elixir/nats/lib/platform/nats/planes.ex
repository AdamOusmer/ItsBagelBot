# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.Planes do
  @moduledoc "Routes RPC to the leaf and JetStream API requests to the hub."

  alias Platform.Nats
  alias Platform.Nats.Settings

  @enforce_keys [:plane, :pid]
  defstruct [:plane, :pid]

  def settings(%{rpc: _, jetstream: _} = endpoints, credentials, tls) do
    Map.new(endpoints, fn {plane, endpoint} ->
      {plane, [Settings.connection(endpoint, credentials, tls)]}
    end)
  end

  def children(%{rpc: _, jetstream: _} = settings, opts \\ []) do
    for plane <- [:jetstream, :rpc] do
      name = connection_name(plane)
      Nats.connection_child(name, name, Map.fetch!(settings, plane), opts)
    end
  end

  @doc "Captures the current connection for subscriptions, monitors and in-flight reads."
  def connection(plane) do
    case Nats.connection_pid(connection_name(plane)) do
      nil -> nil
      pid -> %__MODULE__{plane: plane, pid: pid}
    end
  end

  def request(message, connection \\ :rpc), do: request_on(:rpc, connection, message)

  def jetstream_request(message, connection \\ :jetstream),
    do: request_on(:jetstream, connection, message)

  def publish(connection, subject, body), do: Nats.publish(ref(connection), subject, body)

  def subscribe(connection, subscriber, subject, opts \\ []),
    do: Nats.subscribe(ref(connection), subscriber, subject, opts)

  def unsubscribe(%__MODULE__{pid: pid}, sid), do: Nats.unsubscribe(pid, sid)

  def jetstream_metadata(message), do: Gnat.Jetstream.API.Message.metadata(message)

  defp request_on(plane, connection, {subject, body, opts}),
    do: Nats.request(request_ref(plane, connection), subject, body, opts)

  defp request_ref(plane, plane), do: Nats.connection_pid(connection_name(plane))
  defp request_ref(plane, %__MODULE__{plane: plane} = connection), do: ref(connection)

  defp ref(%__MODULE__{pid: pid}), do: pid
  defp ref(plane), do: connection_name(plane)

  def connection_name(:rpc), do: :gnat_rpc
  def connection_name(:jetstream), do: :gnat_jetstream
end
