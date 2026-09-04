# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.HealthRpc do
  @moduledoc """
  Side-effect-free NATS request/reply endpoint used by the admin RPC latency
  panel and by sesame, which folds this reply into the Twitch vertical of the
  public status page. A reply proves the ingress RPC connection, account route
  and consumer dispatcher are live without asking the shard registry for a full
  snapshot.

  The body is the same report /status serves (Ingress.Health), not the bare
  `ok: true` it used to be: an aggregator that can only see a boolean would
  publish a degraded ingress as healthy and the impairment would disappear from
  the rollup. `ok` survives because the admin console panel reads it, and now
  means "not down" — the same line /readyz draws.
  """

  use Gnat.Server
  require Logger

  alias Ingress.Health

  @impl true
  def request(%{body: _body}) do
    report = Health.report()

    {:reply,
     Jason.encode!(%{
       service: report.service,
       ok: Health.up?(report),
       status: report.status,
       checks: report.checks
     })}
  end

  @impl true
  def error(_message, error) do
    Logger.error("health rpc error: #{inspect(error)}")
    :ok
  end
end
