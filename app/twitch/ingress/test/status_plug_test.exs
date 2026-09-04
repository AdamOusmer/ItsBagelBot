# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.StatusPlugTest do
  use ExUnit.Case, async: true
  import Plug.Test
  import Plug.Conn

  alias Ingress.StatusPlug

  defp call(path), do: StatusPlug.call(conn(:get, path), [])

  test "healthz is process liveness only" do
    conn = call("/healthz")
    assert conn.status == 200
  end

  test "unknown path is 404" do
    assert call("/nope").status == 404
  end

  # No NATS connection runs under the test supervisor, so both planes report
  # down — which is exactly the contract worth pinning: readyz refuses traffic
  # and /status says down with both checks named.
  test "readyz is 503 while the NATS planes are down" do
    assert call("/readyz").status == 503
  end

  test "status reports both planes as JSON" do
    conn = call("/status")
    assert conn.status == 503
    assert get_resp_header(conn, "content-type") |> hd() =~ "application/json"
    assert get_resp_header(conn, "cache-control") == ["no-store"]

    body = Jason.decode!(conn.resp_body)
    assert body["service"] == "ingress"
    assert body["status"] == "down"
    assert Enum.map(body["checks"], & &1["name"]) == ["nats_rpc", "nats_bus"]
    refute Enum.any?(body["checks"], & &1["ok"])
  end

  test "status is ok when both connection names are registered" do
    Process.register(spawn(fn -> Process.sleep(:infinity) end), :gnat)
    Process.register(spawn(fn -> Process.sleep(:infinity) end), :gnat_bus)

    on_exit(fn ->
      for name <- [:gnat, :gnat_bus] do
        case Process.whereis(name) do
          pid when is_pid(pid) -> Process.exit(pid, :kill)
          nil -> :ok
        end
      end
    end)

    conn = call("/status")
    assert conn.status == 200
    body = Jason.decode!(conn.resp_body)
    assert body["status"] == "ok"
    assert Enum.all?(body["checks"], & &1["ok"])
    assert call("/readyz").status == 200
  end
end
