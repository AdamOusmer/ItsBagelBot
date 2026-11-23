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

  @spec overlay_session(pos_integer()) :: String.t()
  def overlay_session(user_id), do: "overlays.session.#{user_id}"

  @spec overlay_revised(pos_integer()) :: String.t()
  def overlay_revised(overlay_id), do: "controls.overlay.revised.#{overlay_id}"

  @spec overlay_revoked(pos_integer()) :: String.t()
  def overlay_revoked(overlay_id), do: "controls.overlay.revoked.#{overlay_id}"

  @spec overlay_chat(pos_integer()) :: String.t()
  def overlay_chat(user_id), do: "channel.chat.#{user_id}"

  @spec overlay_feed(pos_integer()) :: String.t()
  def overlay_feed(user_id), do: "overlay.feed.#{user_id}.>"

  @spec overlay_offer_reply() :: String.t()
  def overlay_offer_reply,
    do:
      "overlays.session.reply." <>
        Base.url_encode64(:crypto.strong_rand_bytes(12), padding: false)
end
