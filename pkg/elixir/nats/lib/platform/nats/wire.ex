# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Nats.Wire do
  @moduledoc "Timed binary/iodata publish path preserving NATS headers and telemetry."
  def publish(conn, {subject, body, opts}, timeout)
      when is_integer(timeout) and timeout in 1..60_000 do
    with {:ok, opts} <- command_opts(subject, opts) do
      started = :erlang.monotonic_time()
      result = GenServer.call(conn, {:pub, subject, body, opts}, timeout)
      emit_pub(subject, started)
      result
    end
  catch
    :exit, _ -> {:error, :not_connected}
  end

  def publish(_, _, _), do: {:error, :invalid_timeout}

  @doc "Pipelined ordered publish, one result per message. Call it from a process with a short mailbox."
  def publish_many(conn, messages, timeout)
      when is_integer(timeout) and timeout in 1..60_000 do
    deadline = :erlang.monotonic_time(:millisecond) + timeout

    messages
    |> Enum.map(&send_pub(conn, &1))
    |> Enum.map(&await_pub(&1, deadline))
  end

  def publish_many(_, messages, _), do: Enum.map(messages, fn _ -> {:error, :invalid_timeout} end)

  defp send_pub(conn, {subject, body, opts}) do
    with {:ok, opts} <- command_opts(subject, opts) do
      started = :erlang.monotonic_time()
      {subject, started, :gen_server.send_request(conn, {:pub, subject, body, opts})}
    end
  end

  defp await_pub({:error, _reason} = error, _deadline), do: error

  defp await_pub({subject, started, request}, deadline) do
    case :gen_server.receive_response(request, {:abs, deadline}) do
      {:reply, result} ->
        emit_pub(subject, started)
        result

      _down_or_timeout ->
        {:error, :not_connected}
    end
  end

  defp emit_pub(subject, started) do
    latency = :erlang.monotonic_time() - started
    :telemetry.execute([:gnat, :pub], %{latency: latency}, %{topic: subject})
  end

  # Gnat.Command.build/4 runs in the connection process and matches only
  # [headers:, reply_to:] in this order; any other shape crashes the connection.
  defp command_opts(subject, opts) do
    with true <- field?(subject),
         true <- Keyword.keyword?(opts),
         [] <- Keyword.keys(opts) -- [:headers, :reply_to],
         true <- field?(Keyword.get(opts, :reply_to, "reply")),
         true <- headers?(Keyword.get(opts, :headers, [])) do
      {:ok, encode_headers(opts) ++ Keyword.take(opts, [:reply_to])}
    else
      _ -> {:error, :invalid_publish}
    end
  end

  defp encode_headers(opts) do
    case Keyword.fetch(opts, :headers) do
      {:ok, headers} -> [headers: Gnat.Headers.encode(headers)]
      :error -> []
    end
  end

  defp headers?(headers) when is_list(headers) do
    Enum.all?(headers, fn
      {name, value} -> field?(name) and is_binary(value) and not line_break?(value)
      _ -> false
    end)
  end

  defp headers?(_), do: false

  defp field?(value) when is_binary(value) and value != "",
    do: :binary.match(value, pattern(:field, [" ", "\t", "\r", "\n"])) == :nomatch

  defp field?(_), do: false

  defp line_break?(value),
    do: :binary.match(value, pattern(:line_break, ["\r", "\n"])) != :nomatch

  defp pattern(name, bytes) do
    key = {__MODULE__, name}

    with nil <- :persistent_term.get(key, nil) do
      compiled = :binary.compile_pattern(bytes)
      :persistent_term.put(key, compiled)
      compiled
    end
  end
end
