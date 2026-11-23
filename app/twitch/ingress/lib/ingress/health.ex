# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Health do
  alias Platform.Health

  @service "ingress"

  defdelegate up?(report), to: Health
  defdelegate http_status(status), to: Health
  defdelegate aggregate(checks), to: Health

  @spec report() :: Health.report()
  def report, do: Health.report(@service, checks())

  @spec checks() :: [Health.check()]
  def checks do
    [Health.check("nats_rpc", :gnat), Health.check("nats_bus", :gnat_bus)]
  end
end
