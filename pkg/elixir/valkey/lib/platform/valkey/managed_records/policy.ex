# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Valkey.ManagedRecords.Policy do
  @moduledoc "Validated declarative record schema, state transitions, and retention bounds."
  @enforce_keys [
    :fields,
    :states,
    :initial,
    :clear_on_add,
    :sticky_reasons,
    :promote_reason,
    :dedup_seconds,
    :history_seconds,
    :lease_ms
  ]
  defstruct @enforce_keys ++ [:payload]

  @fields ~w(generation state enabled slot moved_at load stop_reason subscription_id received error display_name)a
  @states ~w(pending stopping disabled receiving failed removed promoted)a

  def new!(opts) do
    policy = struct!(__MODULE__, opts)

    if valid?(policy) do
      payload = Platform.Sift.encode!(Map.from_struct(policy) |> Map.delete(:payload))
      %{policy | payload: payload}
    else
      raise ArgumentError, "invalid managed record policy"
    end
  end

  def encode(%__MODULE__{payload: payload})
      when is_binary(payload) and byte_size(payload) <= 8_192,
      do: {:ok, payload}

  def encode(_), do: {:error, :invalid}

  def admission_args(%__MODULE__{} = policy),
    do: [
      policy.fields.generation,
      policy.fields.enabled,
      policy.fields.state,
      policy.states.receiving,
      policy.fields.received,
      policy.dedup_seconds
    ]

  defp valid?(p) do
    Enum.all?([
      names?(p.fields, @fields),
      names?(p.states, @states),
      initial?(p.initial),
      strings?(p.clear_on_add, 32),
      strings?(p.sticky_reasons, 16),
      text?(p.promote_reason),
      integer?(p.dedup_seconds, 86_400),
      integer?(p.history_seconds, 31_536_000),
      integer?(p.lease_ms, 86_400_000)
    ])
  end

  defp names?(value, required) when is_map(value),
    do:
      Enum.sort(Map.keys(value)) == Enum.sort(required) and Enum.all?(Map.values(value), &text?/1)

  defp names?(_, _), do: false

  defp initial?(value) when is_map(value) and map_size(value) in 1..32,
    do: Enum.all?(value, fn {key, val} -> text?(key) and initial_value?(val) end)

  defp initial?(_), do: false

  defp initial_value?(value) when is_integer(value), do: value in 0..1_000_000_000
  defp initial_value?(value), do: text?(value)

  defp strings?(value, max) when is_list(value),
    do: length(value) <= max and Enum.all?(value, &text?/1)

  defp strings?(_, _), do: false
  defp text?(value), do: is_binary(value) and byte_size(value) in 1..128 and String.valid?(value)
  defp integer?(value, max), do: is_integer(value) and value in 1..max
end
