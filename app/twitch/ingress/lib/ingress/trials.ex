# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Trials do
  alias Ingress.TrialConduit
  alias Ingress.TrialValkey, as: VK
  alias Platform.Valkey.ManagedRecords, as: Records

  @policy Platform.Valkey.ManagedRecords.Policy.new!(
            fields: %{
              generation: "generation",
              state: "state",
              enabled: "enabled",
              slot: "slot",
              moved_at: "moved_at",
              load: "load",
              stop_reason: "stop_reason",
              subscription_id: "subscription_id",
              received: "received",
              error: "error",
              display_name: "display_name"
            },
            states: %{
              pending: "pending",
              stopping: "stopping",
              disabled: "disabled",
              receiving: "receiving",
              failed: "failed",
              removed: "removed",
              promoted: "promoted"
            },
            initial: %{
              "state" => "pending",
              "enabled" => 1,
              "received" => 0,
              "decoded" => 0,
              "processed" => 0,
              "failed" => 0,
              "retried" => 0,
              "blocked" => 0,
              "latency_samples" => 0,
              "latency_total_ms" => 0
            },
            clear_on_add: ["error", "stop_reason", "display_name"],
            sticky_reasons: ["promoted", "removed"],
            promote_reason: "promoted",
            dedup_seconds: 120,
            history_seconds: 604_800,
            lease_ms: 60_000
          )
  def policy, do: @policy
  @channel_prefix "trial:channel:"

  @max_channels 30

  @members "trial:desired"
  @revision "trial:revision"
  @generation "trial:generation"
  @history "trial:history"
  @socket_target "trial:socket_target"

  def valid_id?(id) when is_binary(id) do
    String.match?(id, ~r/^[1-9][0-9]{0,19}$/) &&
      String.to_integer(id) <= 18_446_744_073_709_551_615
  end

  def valid_id?(_), do: false

  def max_channels, do: @max_channels

  def add(id) do
    with true <- valid_id?(id),
         {:ok, %{trials: rows, socket_target: target}} <- list(),
         slot = TrialConduit.slot_for_new(rows, target),
         {:ok, value} <-
           records(:add, [@members, @revision, @generation, @history], [
             id,
             slot,
             @max_channels,
             @channel_prefix
           ]) do
      case value do
        "full" -> {:error, "full"}
        "duplicate" -> {:ok, "duplicate"}
        generation -> {:ok, generation}
      end
    else
      false -> {:error, "invalid_id"}
      {:error, _} -> {:error, "unavailable"}
    end
  end

  def move(id, generation, slot),
    do:
      records(:move, [@members, @revision], [
        id,
        generation,
        slot,
        System.system_time(:second),
        @channel_prefix
      ])

  def set_socket_target(target), do: Platform.Valkey.set(VK, @socket_target, target)

  def record_load(id, generation, slot, load),
    do: records(:load, [@channel_prefix <> id], [generation, slot, load])

  def stop(id, reason) when reason in ["promoted", "removed", "registration_check_unavailable"] do
    case records(:stop, [@members, @revision], [id, reason, @channel_prefix]) do
      {:ok, "changed"} -> {:ok, :changed}
      {:ok, "unchanged"} -> {:ok, :unchanged}
      {:ok, "absent"} -> {:ok, :absent}
      _ -> {:error, "unavailable"}
    end
  end

  def set_enabled(id, enabled) when is_boolean(enabled) do
    value = if enabled, do: "1", else: "0"

    case records(:set_enabled, [@members, @revision], [
           id,
           value,
           @channel_prefix
         ]) do
      {:ok, "absent"} -> {:error, "not_found"}
      {:ok, "stopping"} -> {:error, "stopping"}
      {:ok, state} -> {:ok, state}
      {:error, _} -> {:error, "unavailable"}
    end
  end

  def finish(id),
    do:
      records(:finish, [@members, @revision, @history], [
        id,
        System.system_time(:second),
        @channel_prefix
      ])

  def list do
    cutoff = System.system_time(:second) - @policy.history_seconds

    with {:ok, ids} <- Platform.Valkey.set_members(VK, @members),
         {:ok, _} <- Platform.Valkey.sorted_remove_before(VK, @history, cutoff),
         {:ok, history_ids} <- Platform.Valkey.sorted_members(VK, @history),
         {:ok, revision} <- Platform.Valkey.get(VK, @revision),
         {:ok, target} <- Platform.Valkey.get(VK, @socket_target),
         {:ok, rows} <- rows(Enum.uniq(ids ++ history_ids)) do
      {:ok,
       %{
         version: String.to_integer(revision || "0"),
         active_count: length(ids),
         max_channels: @max_channels,
         socket_target: TrialConduit.clamp(String.to_integer(target || "1")),
         trials: rows
       }}
    end
  end

  defp rows(ids) do
    Enum.reduce_while(ids, {:ok, []}, fn id, {:ok, acc} ->
      case Platform.Valkey.hash_get_all(VK, @channel_prefix <> id) do
        {:ok, fields} ->
          row = fields |> Enum.chunk_every(2) |> Map.new(fn [k, v] -> {k, v} end)

          {:cont,
           {:ok,
            [
              %{
                broadcaster_id: id,
                state: row["state"] || "pending",
                enabled: row["enabled"] != "0",
                slot: slot(row["slot"]),
                load: integer(row["load"]),
                moved_at: integer(row["moved_at"]),
                error: row["error"],
                generation: row["generation"],
                display_name: row["display_name"],
                subscription_id: row["subscription_id"],
                session_id: row["session_id"],
                stop_reason: row["stop_reason"],
                received: integer(row["received"]),
                decoded: integer(row["decoded"]),
                processed: integer(row["processed"]),
                failed: integer(row["failed"]),
                retried: integer(row["retried"]),
                blocked_actions: integer(row["blocked"]),
                average_processing_latency_ms: average_latency(row)
              }
              | acc
            ]}}

        error ->
          {:halt, error}
      end
    end)
    |> case do
      {:ok, rows} -> {:ok, Enum.reverse(rows)}
      error -> error
    end
  end

  defp slot(nil), do: 0
  defp slot(value), do: String.to_integer(value)

  defp integer(nil), do: 0
  defp integer(value), do: String.to_integer(value)

  defp average_latency(row) do
    samples = integer(row["latency_samples"])
    if samples == 0, do: 0, else: div(integer(row["latency_total_ms"]), samples)
  end

  def field(id, name, value), do: Platform.Valkey.hash_set(VK, @channel_prefix <> id, name, value)

  def fail(id, generation, reason),
    do: records(:fail, [@channel_prefix <> id], [generation, reason])

  def display_name(id, generation, name) when is_binary(name) and name != "" do
    records(:display_name, [@channel_prefix <> id], [generation, name])
  end

  def increment(id, name), do: Platform.Valkey.hash_increment(VK, @channel_prefix <> id, name, 1)

  def admit(id, generation, chat_id) do
    case Records.run(VK, @policy, :admit, admit_operation(id, generation, chat_id)) do
      {:ok, value} -> admit_result(value)
      _ -> :unavailable
    end
  end

  def admit_operation(id, generation, chat_id),
    do:
      {[@members, @channel_prefix <> id, "trial:dedup:" <> id <> ":" <> chat_id],
       [id, generation]}

  def admit_result("first"), do: :first
  def admit_result("duplicate"), do: :duplicate
  def admit_result("inactive"), do: :inactive
  def admit_result(_error), do: :unavailable

  def owners do
    Platform.Valkey.get_many(VK, Enum.map(TrialConduit.slots(), &lease_key/1))
  end

  def acquire(owner, slot \\ 0) do
    with {:ok, epoch} <- Platform.Valkey.increment(VK, "trial:owner_epoch"),
         {:ok, result} <-
           Platform.Valkey.set_if_absent_ms(
             VK,
             lease_key(slot),
             "#{epoch}:#{owner}",
             @policy.lease_ms
           ) do
      if result == "OK", do: {:ok, epoch}, else: {:error, :owned}
    end
  end

  def renew(owner, epoch, slot \\ 0), do: owner_script(:renew, owner, epoch, slot)

  def owner_session(owner, epoch, session_id, slot \\ 0) when is_binary(session_id),
    do:
      records(:owner_session, [lease_key(slot), session_key(slot)], [
        "#{epoch}:#{owner}",
        epoch,
        session_id
      ])

  def clear_owner_session(owner, epoch, slot \\ 0),
    do: owner_script(:clear_owner_session, owner, epoch, slot)

  def release(owner, epoch, slot), do: owner_script(:release, owner, epoch, slot)

  defp owner_script(operation, owner, epoch, slot),
    do:
      records(operation, [lease_key(slot), session_key(slot)], [
        "#{epoch}:#{owner}",
        epoch
      ])

  defp records(operation, keys, args), do: Records.run(VK, @policy, operation, {keys, args})

  # Slot 0 keeps the single-socket key names that outgress already reads.
  defp lease_key(0), do: "trial:owner"
  defp lease_key(slot), do: "trial:owner:#{slot}"
  defp session_key(0), do: "trial:owner_session"
  defp session_key(slot), do: "trial:owner_session:#{slot}"
end
