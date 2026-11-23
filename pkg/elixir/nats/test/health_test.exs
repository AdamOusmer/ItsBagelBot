# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.HealthTest do
  use ExUnit.Case, async: true

  alias Platform.Health

  defp check(name, ok, extra \\ %{}), do: Map.merge(%{name: name, ok: ok, latency_ms: 0}, extra)

  defp aggregates do
    [
      {[check("a", true), check("b", true)], "ok"},
      {[check("a", true), check("b", false)], "down"},
      {[check("a", true), check("b", false, %{optional: true})], "degraded"},
      {[check("a", false), check("b", false, %{optional: true})], "down"}
    ]
  end

  test "a critical failure is down, an optional one is only degraded" do
    for {checks, status} <- aggregates() do
      assert Health.aggregate(checks) == status
    end
  end

  test "http status and readiness follow the verdict" do
    for {status, code, up} <- [{"ok", 200, true}, {"degraded", 207, true}, {"down", 503, false}] do
      assert {Health.http_status(status), Health.up?(%{status: status})} == {code, up}
    end
  end

  test "report runs probes and connection checks under the service name" do
    report =
      Health.report("svc", [
        Health.check("live", self()),
        Health.check("absent", :no_such_connection),
        {"draining", fn -> false end},
        {"attached", fn -> true end},
        check("given", true)
      ])

    assert report.service == "svc"
    assert report.status == "down"

    assert Enum.map(report.checks, &{&1.name, &1.ok}) == [
             {"live", true},
             {"absent", false},
             {"draining", false},
             {"attached", true},
             {"given", true}
           ]
  end

  test "a probe that returns a non-boolean is not ok" do
    assert %{ok: false} = Health.check("odd", fn -> :maybe end)
  end
end
