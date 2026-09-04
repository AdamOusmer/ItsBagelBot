# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.HealthTest do
  # Not async: StatusPlugTest registers :gnat and :gnat_bus globally, and the
  # report/0 case below reads those same names.
  use ExUnit.Case, async: false

  alias Ingress.Health

  defp check(name, ok, extra \\ %{}), do: Map.merge(%{name: name, ok: ok, latency_ms: 0}, extra)

  test "a critical failure is down, an optional one is only degraded" do
    assert Health.aggregate([check("a", true), check("b", true)]) == "ok"
    assert Health.aggregate([check("a", true), check("b", false)]) == "down"

    assert Health.aggregate([check("a", true), check("b", false, %{optional: true})]) ==
             "degraded"

    # A critical failure wins over an optional one, whatever the order.
    assert Health.aggregate([check("a", false), check("b", false, %{optional: true})]) == "down"
  end

  test "the http code carries the same verdict as the body" do
    assert Health.http_status("ok") == 200
    assert Health.http_status("degraded") == 207
    assert Health.http_status("down") == 503
  end

  test "degraded is up: it still takes traffic" do
    assert Health.up?(%{status: "ok"})
    assert Health.up?(%{status: "degraded"})
    refute Health.up?(%{status: "down"})
  end

  test "report names both NATS planes" do
    report = Health.report()

    assert report.service == "ingress"
    assert Enum.map(report.checks, & &1.name) == ["nats_rpc", "nats_bus"]
    assert Enum.all?(report.checks, &(&1.latency_ms == 0))
    assert report.status == Health.aggregate(report.checks)
  end
end
