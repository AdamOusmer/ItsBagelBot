# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Nats.Wire do
  @moduledoc "Timed binary/iodata publish path preserving NATS headers and telemetry."
  def publish(conn, {subject, body, opts}, timeout)
      when is_integer(timeout) and timeout in 1..60_000 do
    with {:ok, opts} <- command_opts(subject, opts) do
      started = :erlang.monotonic_time()
      result = GenServer.call(conn, {:pub, subject, body, opts}, timeout)
      latency = :erlang.monotonic_time() - started
      :telemetry.execute([:gnat, :pub], %{latency: latency}, %{topic: subject})
      result
    end
  catch
    :exit, _ -> {:error, :not_connected}
  end

  def publish(_, _, _), do: {:error, :invalid_timeout}

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
    do: :binary.match(value, [" ", "\t", "\r", "\n"]) == :nomatch

  defp field?(_), do: false

  defp line_break?(value), do: :binary.match(value, ["\r", "\n"]) != :nomatch
end
