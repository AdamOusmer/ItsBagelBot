# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Nats do
  @moduledoc "Shared, bounded NATS connection, request and subscription operations."
  @default_timeout 5_000

  defguardp named(value) when is_atom(value) and not is_nil(value)

  def connection_child(id, name, settings, opts \\ []) do
    check!(
      named(name) and maps?(settings) and backoff_only?(opts),
      "invalid NATS connection configuration"
    )

    backoff = Keyword.get(opts, :backoff_period, 4_000)
    check!(is_integer(backoff) and backoff in 100..120_000, "invalid NATS backoff")

    Supervisor.child_spec(
      {Gnat.ConnectionSupervisor,
       %{name: name, backoff_period: backoff, connection_settings: settings}},
      id: id
    )
  end

  def consumer_child(id, connection, module, subscriptions) do
    check!(
      named(connection) and named(module) and maps?(subscriptions),
      "invalid NATS consumer configuration"
    )

    Supervisor.child_spec(
      {Gnat.ConsumerSupervisor,
       %{connection_name: connection, module: module, subscription_topics: subscriptions}},
      id: id
    )
  end

  def publish(connection, subject, body, opts \\ []) do
    Gnat.pub(connection, subject, body, opts)
  catch
    :exit, _ -> {:error, :not_connected}
  end

  def request(connection, subject, body, opts \\ []) do
    timeout = Keyword.get(opts, :receive_timeout, @default_timeout)

    if is_integer(timeout) and timeout in 1..60_000 do
      Gnat.request(
        connection,
        subject,
        body,
        Keyword.put_new(opts, :receive_timeout, @default_timeout)
      )
    else
      {:error, :invalid_timeout}
    end
  catch
    :exit, _ -> {:error, :not_connected}
  end

  def subscribe(connection, subscriber, subject, opts \\ []) do
    Gnat.sub(connection, subscriber, subject, opts)
  catch
    :exit, _ -> {:error, :not_connected}
  end

  def unsubscribe(connection, sid) do
    Gnat.unsub(connection, sid)
  catch
    :exit, _ -> {:error, :not_connected}
  end

  def connected?(connection) when is_atom(connection), do: is_pid(Process.whereis(connection))
  def connected?(connection) when is_pid(connection), do: Process.alive?(connection)
  def connected?(_), do: false
  def connection_pid(connection) when is_atom(connection), do: Process.whereis(connection)

  def connection_pid(connection) when is_pid(connection),
    do: if(Process.alive?(connection), do: connection)

  def server_name(connection) do
    Gnat.server_info(connection).server_name
  catch
    :exit, _ -> nil
  end

  def stop(connection) do
    Gnat.stop(connection)
  catch
    :exit, _ -> :ok
  end

  def probe_server_name(settings) do
    case Gnat.start_link(settings) do
      {:ok, pid} ->
        try do
          Gnat.server_info(pid).server_name
        after
          Gnat.stop(pid)
        end

      _ ->
        nil
    end
  catch
    :exit, _ -> nil
  end

  defp maps?([_ | _] = values), do: Enum.all?(values, &is_map/1)
  defp maps?(_values), do: false

  defp backoff_only?([]), do: true
  defp backoff_only?(backoff_period: _), do: true
  defp backoff_only?(_opts), do: false

  defp check!(true, _message), do: :ok
  defp check!(false, message), do: raise(ArgumentError, message)
end
