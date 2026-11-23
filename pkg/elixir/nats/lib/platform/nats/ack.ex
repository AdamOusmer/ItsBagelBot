# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.Ack do
  @moduledoc "JetStream acknowledgements and ack-subject validation."

  alias Platform.Nats.Planes

  for {name, body} <- [ack: "+ACK", progress: "+WPI", term: "+TERM", nak: "-NAK"] do
    def unquote(name)(connection, subject), do: Planes.publish(connection, subject, unquote(body))
  end

  def nak(connection, subject, delay_ns) when is_integer(delay_ns) and delay_ns >= 0,
    do: Planes.publish(connection, subject, "-NAK " <> Jason.encode!(%{delay: delay_ns}))

  def subject?("$JS.ACK." <> tokens, consumers),
    do: tokens |> String.split(".") |> tokens?(consumers)

  def subject?(_subject, _consumers), do: false

  defp tokens?([stream, durable | counters], consumers) when length(counters) == 5,
    do: settled?(consumers, {stream, durable}, counters)

  defp tokens?([domain, hash, stream, durable | rest], consumers) when length(rest) == 6 do
    {counters, [random]} = Enum.split(rest, 5)

    settled?(consumers, {stream, durable}, counters) and
      Enum.all?([domain, hash, random], &token?/1)
  end

  defp tokens?(_tokens, _consumers), do: false

  defp settled?(consumers, consumer, counters),
    do: consumer in consumers and numeric?(counters)

  defp token?(token), do: Regex.match?(~r/\A[A-Za-z0-9_-]+\z/, token)

  defp numeric?(tokens), do: Enum.all?(tokens, &match?({_n, ""}, Integer.parse(&1)))
end
