# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.HealthRpc do
  use Ingress.RpcServer, log: "health rpc"

  alias Ingress.{Health, JSON}

  @impl true
  def request(%{body: _body}) do
    report = Health.report()

    {:reply,
     JSON.encode(%{
       service: report.service,
       ok: Health.up?(report),
       status: report.status,
       checks: report.checks
     })}
  end
end
