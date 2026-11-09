# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.NewRelic do
  @moduledoc "Shared New Relic boundary. Callers provide fixed metric and span names and an explicit attribute policy. Disabled without a license."
  @trace_headers ~w(traceparent tracestate newrelic)

  def runtime_config(default_name, opts \\ []) do
    logs = Keyword.get(opts, :logs_in_context, :disabled)
    unless logs in [:disabled, :forwarder], do: raise("invalid New Relic log policy")
    errors = Keyword.get(opts, :error_collector, false)
    unless is_boolean(errors), do: raise("invalid New Relic error policy")
    log_env = System.get_env("NEW_RELIC_LOGS_IN_CONTEXT")

    unsafe_features =
      ~w(NEW_RELIC_PLUG_INSTRUMENTATION_ENABLED NEW_RELIC_REDIX_INSTRUMENTATION_ENABLED NEW_RELIC_FINCH_INSTRUMENTATION_ENABLED NEW_RELIC_ECTO_INSTRUMENTATION_ENABLED NEW_RELIC_PHOENIX_INSTRUMENTATION_ENABLED NEW_RELIC_PHOENIX_LIVE_VIEW_INSTRUMENTATION_ENABLED NEW_RELIC_OBAN_INSTRUMENTATION_ENABLED NEW_RELIC_ABSINTHE_INSTRUMENTATION_ENABLED NEW_RELIC_SQL_COLLECTION_ENABLED NEW_RELIC_DB_QUERY_COLLECTION_ENABLED NEW_RELIC_QUERY_COLLECTION_ENABLED NEW_RELIC_FUNCTION_ARGUMENT_COLLECTION_ENABLED NEW_RELIC_STACKTRACE_ARGUMENT_COLLECTION_ENABLED NEW_RELIC_EXTENDED_ATTRIBUTES_ENABLED NEW_RELIC_ERROR_COLLECTOR_ENABLED)

    if Enum.any?(unsafe_features, &(System.get_env(&1) == "true")) or
         log_env not in [nil, Atom.to_string(logs)] do
      raise "New Relic automatic capture or log forwarding is prohibited by telemetry policy"
    end

    license = System.get_env("NEW_RELIC_LICENSE_KEY", "")

    [
      app_name: System.get_env("NEW_RELIC_APP_NAME", default_name),
      license_key: if(license == "", do: nil, else: license),
      harvest_enabled: license != "",
      logs_in_context: logs,
      plug_instrumentation_enabled: false,
      redix_instrumentation_enabled: false,
      finch_instrumentation_enabled: false,
      ecto_instrumentation_enabled: false,
      phoenix_instrumentation_enabled: false,
      phoenix_live_view_instrumentation_enabled: false,
      oban_instrumentation_enabled: false,
      absinthe_instrumentation_enabled: false,
      query_collection_enabled: false,
      function_argument_collection_enabled: false,
      stacktrace_argument_collection_enabled: false,
      extended_attributes_enabled: false,
      error_collector_enabled: errors
    ]
  end

  def enabled?, do: NewRelic.Config.enabled?() == true

  def increment(name, value) do
    if enabled?() do
      safe(fn -> NewRelic.increment_custom_metric(name, value) end)
    else
      :ok
    end
  end

  def event(type, attrs, policy \\ %{}) when is_map(attrs) and is_map(policy) do
    clean = attrs |> Map.to_list() |> safe_attributes(policy) |> Map.new()
    if enabled?(), do: safe(fn -> NewRelic.report_custom_event(type, clean) end), else: :ok
  end

  def transaction(category, name, attrs, fun, policy \\ %{}) when is_function(fun, 0) do
    if enabled?() do
      safe(fn -> NewRelic.start_transaction(category, name) end)
      safe(fn -> NewRelic.add_attributes(safe_attributes(attrs, policy)) end)

      try do
        fun.()
      after
        safe(fn -> NewRelic.stop_transaction() end)
      end
    else
      fun.()
    end
  end

  def span(name, attrs, fun, policy \\ %{}) when is_function(fun, 0) do
    if enabled?() do
      id = make_ref()

      safe(fn ->
        NewRelic.Tracer.Direct.start_span(id, name, attributes: safe_attributes(attrs, policy))
      end)

      try do
        fun.()
      after
        safe(fn -> NewRelic.Tracer.Direct.stop_span(id) end)
      end
    else
      fun.()
    end
  end

  def add_span_attributes(attrs, policy \\ %{}) do
    if enabled?(),
      do: safe(fn -> NewRelic.add_span_attributes(safe_attributes(attrs, policy)) end),
      else: :ok
  end

  def trace_headers do
    if enabled?() do
      try do
        :other
        |> NewRelic.distributed_trace_headers()
        |> Enum.filter(fn {key, _} -> String.downcase(to_string(key)) in @trace_headers end)
      rescue
        _ -> []
      end
    else
      []
    end
  end

  defp safe_attributes(attrs, policy) when is_list(attrs) and is_map(policy) do
    Enum.filter(attrs, fn {key, value} ->
      case Map.get(policy, key) do
        :number -> is_number(value)
        {:enum, values} when is_list(values) -> value in values
        _ -> false
      end
    end)
  end

  defp safe_attributes(_, _), do: []

  defp safe(fun) do
    try do
      fun.()
      :ok
    rescue
      _ -> {:error, :unavailable}
    catch
      _, _ -> {:error, :unavailable}
    end
  end
end
