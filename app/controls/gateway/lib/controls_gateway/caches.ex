# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Caches do
  @moduledoc "Projector resolve and check caches kept fresh by the overlays invalidation feed."

  alias Platform.Cache

  @feed ControlsGateway.Invalidations
  @names [:overlay_resolve, :tts_resolve, :tts_check]
  @ttl_ms 30_000
  @stale_ms 300_000
  @capacity 4_096

  def child_spec(opts) do
    %{
      id: __MODULE__,
      type: :supervisor,
      start: {Supervisor, :start_link, [children(opts), [strategy: :rest_for_one]]}
    }
  end

  defp children(opts) do
    ttl_ms = Keyword.get(opts, :ttl_ms, @ttl_ms)

    [{Platform.Invalidations, name: @feed, plane: :rpc, prefix: "cache.invalidate"}] ++
      Enum.map(@names, &cache(&1, ttl_ms))
  end

  defp cache(name, ttl_ms) do
    {Cache,
     name: name,
     ttl_ms: ttl_ms,
     stale_ms: @stale_ms,
     capacity: @capacity,
     stale_if: &positive?/1,
     invalidate_on: {@feed, &dropped(name, &1), ["overlays"]}}
  end

  defp positive?(value), do: value not in [:not_found, false]

  defp dropped(_name, %{id: :all}), do: :all
  defp dropped(:tts_resolve, _event), do: :all
  defp dropped(:tts_check, %{id: user_id}), do: {:where, &owned_by?(&1, user_id)}
  defp dropped(_name, %{keys: keys}), do: keys

  defp owned_by?({user_id, _generation}, user_id), do: true
  defp owned_by?(_key, _user_id), do: false
end
