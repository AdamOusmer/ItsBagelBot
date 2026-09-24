# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Config.Admin do
  def admin_subject, do: Application.fetch_env!(:ingress, :admin_subject)

  def scale_subject, do: Application.fetch_env!(:ingress, :scale_subject)

  def autoscale_subject, do: Application.fetch_env!(:ingress, :autoscale_subject)

  def conduit_subject, do: Application.fetch_env!(:ingress, :conduit_subject)

  def rpc_health_subject, do: Application.fetch_env!(:ingress, :rpc_health_subject)
end
