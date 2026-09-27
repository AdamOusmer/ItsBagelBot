# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ClusterDiscovery do
  @moduledoc false

  # libcluster checks MFA exports without loading their module first.
  def disconnect_mfa, do: {__MODULE__, :retain_connection, []}

  # DNS discovers peers; an omitted address is not evidence that a connected
  # BEAM node died. A unilateral disconnect can trigger global's overlapping
  # partition protection across healthy pods during a rollout. Actual node
  # disconnection still reaches Horde through its normal node monitors.
  # true acknowledges the DNS removal to libcluster without retrying it.
  def retain_connection(_node), do: true
end
