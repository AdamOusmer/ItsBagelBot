defmodule Ingress.Metrics do
  @moduledoc """
  Thin wrapper over the New Relic agent. Every call is a no-op when the agent
  is disabled (no `NEW_RELIC_LICENSE_KEY`), so instrumentation costs nothing
  in dev and test.

  Counters land under `Custom/Ingress/<name>` in New Relic; lifecycle events
  are queryable as the `IngressEvent` custom event type.
  """

  @doc "Increments a counter, e.g. `count(\"Shard/Reconnects\")`."
  @spec count(String.t(), number()) :: :ok
  def count(name, value \\ 1) do
    NewRelic.increment_custom_metric("Custom/Ingress/" <> name, value)
    :ok
  rescue
    # Metrics must never become part of the service's availability contract.
    # The New Relic API raises while its application/config is unavailable
    # (including a disabled local agent); publishing continues without that
    # sample and the next aggregate flush retries normally.
    _exception -> :ok
  catch
    _kind, _reason -> :ok
  end

  @doc "Reports a lifecycle event with attributes, e.g. shard up/down."
  @spec event(String.t(), map()) :: :ok
  def event(name, attributes \\ %{}) do
    NewRelic.report_custom_event(
      "IngressEvent",
      Map.merge(%{name: name, node: to_string(node())}, attributes)
    )

    :ok
  rescue
    _exception -> :ok
  catch
    _kind, _reason -> :ok
  end
end
