# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Valkey.ManagedRecordsPolicyTest do
  use ExUnit.Case, async: true
  alias Platform.Valkey.ManagedRecords
  alias Platform.Valkey.ManagedRecords.Policy

  defp options do
    [
      fields: %{
        generation: "version",
        state: "phase",
        enabled: "active",
        slot: "partition",
        moved_at: "moved",
        load: "weight",
        stop_reason: "cause",
        subscription_id: "subscription",
        received: "count",
        error: "failure",
        display_name: "label"
      },
      states: %{
        pending: "new",
        stopping: "closing",
        disabled: "paused",
        receiving: "running",
        failed: "fault",
        removed: "gone",
        promoted: "complete"
      },
      initial: %{"phase" => "new", "active" => 1, "count" => 0},
      clear_on_add: ["failure"],
      sticky_reasons: ["complete"],
      promote_reason: "complete",
      dedup_seconds: 30,
      history_seconds: 300,
      lease_ms: 5000
    ]
  end

  test "validates and compiles arbitrary caller schema and policy" do
    policy = Policy.new!(options())
    assert {:ok, payload} = Policy.encode(policy)

    assert {:ok, %{"fields" => %{"state" => "phase"}, "dedup_seconds" => 30}} =
             Platform.Sift.decode(payload)

    assert_raise ArgumentError, fn -> Policy.new!(Keyword.put(options(), :lease_ms, 0)) end
    assert_raise ArgumentError, fn -> Policy.new!(Keyword.put(options(), :initial, %{})) end

    assert_raise ArgumentError, fn ->
      Policy.new!(Keyword.put(options(), :sticky_reasons, List.duplicate("x", 17)))
    end
  end

  test "rejects malformed operation arguments before contacting backend" do
    policy = Policy.new!(options())
    assert {:error, :invalid} = ManagedRecords.run(:absent, policy, :unknown, {["key"], ["arg"]})
    assert {:error, :invalid} = ManagedRecords.run(:absent, policy, :add, {["key"], ["arg"]})
    assert {:error, :invalid} = ManagedRecords.admit_batch(:absent, policy, [:malformed])

    assert {:error, :invalid} =
             ManagedRecords.admit_batch(
               :absent,
               policy,
               List.duplicate({["a", "b", "c"], ["d", "e"]}, 257)
             )
  end

  @tag skip: is_nil(System.get_env("VALKEY_TEST_PORT"))
  test "custom field and state policy runs through Valkey, including a cold script cache" do
    {:ok, conn} =
      Redix.start_link(
        host: "127.0.0.1",
        port: String.to_integer(System.fetch_env!("VALKEY_TEST_PORT"))
      )

    suffix = Base.url_encode64(:crypto.strong_rand_bytes(12), padding: false)
    prefix = "managed-test:" <> suffix <> ":"
    members = prefix <> "members"
    policy = Policy.new!(options())
    assert {:ok, "OK"} = Redix.command(conn, ["SCRIPT", "FLUSH"])

    assert {:ok, "1"} =
             ManagedRecords.run(
               conn,
               policy,
               :add,
               {[members, prefix <> "revision", prefix <> "generation", prefix <> "history"],
                ["id", 2, 3, prefix]}
             )

    assert {:ok, "new"} = Redix.command(conn, ["HGET", prefix <> "id", "phase"])
    assert {:ok, 0} = Redix.command(conn, ["HSET", prefix <> "id", "phase", "running"])
    operation = {[members, prefix <> "id", prefix <> "dedup"], ["id", "1"]}

    assert {:ok, ["first", "duplicate"]} =
             ManagedRecords.admit_batch(conn, policy, [operation, operation])

    assert {:ok, "1"} = Redix.command(conn, ["HGET", prefix <> "id", "count"])

    assert {:ok, "changed"} =
             ManagedRecords.run(
               conn,
               policy,
               :stop,
               {[members, prefix <> "revision"], ["id", "complete", prefix]}
             )

    assert {:ok, "unchanged"} =
             ManagedRecords.run(
               conn,
               policy,
               :stop,
               {[members, prefix <> "revision"], ["id", "other", prefix]}
             )

    assert {:ok, 1} =
             ManagedRecords.run(
               conn,
               policy,
               :finish,
               {[members, prefix <> "revision", prefix <> "history"], ["id", 1, prefix]}
             )

    assert {:ok, "complete"} = Redix.command(conn, ["HGET", prefix <> "id", "phase"])
  end
end
