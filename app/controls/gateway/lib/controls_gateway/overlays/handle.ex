# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Handle do
  alias ControlsGateway.Overlays.Tiers
  alias ControlsGateway.SignedCookie
  alias Platform.Nats.Ack

  def sign(user_id, ack), do: encode(ack) <> "." <> encode(mac(user_id, ack))

  def verify(user_id, handle) when is_binary(handle) do
    with [ack_part, mac_part] <- String.split(handle, "."),
         {:ok, ack} <- Base.url_decode64(ack_part, padding: false),
         {:ok, mac} <- Base.url_decode64(mac_part, padding: false),
         true <- byte_size(mac) == 32 and :crypto.hash_equals(mac, mac(user_id, ack)),
         true <- Ack.subject?(ack, Tiers.consumers()) do
      {:ok, ack}
    else
      _ -> :error
    end
  end

  def verify(_user_id, _handle), do: :error

  defp mac(user_id, ack), do: SignedCookie.mac("#{user_id}\n#{ack}")

  defp encode(bytes), do: Base.url_encode64(bytes, padding: false)
end
