# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Ack do
  alias Platform.Nats.Planes

  def ack(subject), do: publish(subject, "+ACK")
  def progress(subject), do: publish(subject, "+WPI")
  def term(subject), do: publish(subject, "+TERM")
  def nak(subject), do: publish(subject, "-NAK")
  def nak(subject, delay_ns), do: publish(subject, "-NAK " <> Jason.encode!(%{delay: delay_ns}))

  defp publish(subject, body), do: Planes.publish(:jetstream, subject, body)
end
