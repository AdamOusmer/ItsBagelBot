# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Subjects do
  @moduledoc """
  Core NATS subjects. Device identity is server-verified; never built from
  client input other than the device id carried in a verified token.
  """

  @spec device_evt(String.t()) :: String.t()
  def device_evt(device_id), do: "controls.device.#{device_id}.evt"

  @spec device_cmd(String.t()) :: String.t()
  def device_cmd(device_id), do: "controls.device.#{device_id}.cmd"

  @spec device_takeover(String.t()) :: String.t()
  def device_takeover(device_id), do: "controls.device.#{device_id}.takeover"

  @spec revoked(String.t()) :: String.t()
  def revoked(device_id), do: "controls.revoked.#{device_id}"
end
