# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.AtomTableTest do
  use ExUnit.Case, async: false

  alias Ingress.Nats.Publisher.AckPath
  alias Ingress.Pipeline
  alias Platform.Sift

  @inputs 2_000
  @meta %{shard_id: 0, msg_id: "m1", ts: "2026-06-10T00:00:00Z"}

  setup do
    start_supervised!({Task.Supervisor, name: Ingress.BroadcasterCache.TaskSupervisor})
    start_supervised!({Ingress.BroadcasterCache, [loader: fn _id -> {:ok, :standard} end]})
    :ok
  end

  test "input never seen before leaves the atom table alone" do
    handle(0)
    before = :erlang.system_info(:atom_count)

    Enum.each(1..@inputs, &handle/1)

    assert :erlang.system_info(:atom_count) - before < div(@inputs, 20)
  end

  defp handle(n) do
    frame =
      Sift.encode!(%{
        "metadata" => %{"message_type" => "notification", "field_#{n}" => n},
        "payload" => %{
          "subscription" => %{"type" => "channel.kind_#{n}"},
          "event" => %{"broadcaster_user_id" => "b#{n}", "key_#{n}" => "value_#{n}"}
        }
      })

    {:publish, _subject, _message} = Pipeline.route(Sift.decode!(frame)["payload"], @meta)
    {:error, {:pub_ack, _}} = Ingress.Nats.parse_pub_ack(~s({"error":{"code_#{n}":#{n}}}))
    {:single, ^n} = AckPath.parse("_INBOX.ingresspub.token.s.#{n}", "_INBOX.ingresspub.token.")
    Ingress.Metrics.count_drop("Dispatcher/Dropped", "reason_#{n}")
  end
end
