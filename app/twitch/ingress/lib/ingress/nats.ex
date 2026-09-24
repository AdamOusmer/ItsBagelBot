# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats do
  alias Ingress.{JSON, Metrics, Nats.Publisher, Trace}

  @connection :gnat_bus

  @spec publish(String.t(), map()) :: :ok | {:error, term()}
  def publish(subject, payload) do
    case safe_encode(payload) do
      {:ok, json} ->
        case Process.whereis(@connection) do
          nil ->
            Metrics.count_drop("Nats/PublishDropped", :not_connected)
            {:error, :not_connected}

          _pid ->
            Gnat.pub(@connection, subject, json)
        end

      {:error, reason} ->
        Metrics.count_drop("Nats/PublishDropped", :encode_error)
        {:error, {:encode, reason}}
    end
  end

  defp safe_encode(payload) do
    {:ok, JSON.encode(payload)}
  rescue
    error -> {:error, error}
  catch
    kind, reason -> {:error, {kind, reason}}
  end

  @spec publish_acked(String.t(), map()) :: :ok | {:error, term()}
  def publish_acked(subject, payload) do
    json =
      Trace.span("encode", fn ->
        JSON.encode(payload)
      end)

    result =
      Trace.span(
        "nats.publish.admit",
        ["messaging.destination": Trace.destination(subject)],
        fn ->
          result = Publisher.enqueue(subject, json, Trace.trace_headers())
          Trace.add_span_attributes(result: Trace.result(result))
          result
        end
      )

    case result do
      :ok ->
        :ok

      {:error, :overloaded} = error ->
        Metrics.count_drop("Nats/PublishDropped", :overloaded)
        error

      {:error, :not_connected} = error ->
        Metrics.count_drop("Nats/PublishDropped", :not_connected)
        error

      {:error, _reason} = error ->
        Metrics.count_drop("Nats/PublishDropped", :enqueue_error)
        error
    end
  end

  @doc false
  @spec parse_pub_ack(binary()) :: :ok | {:error, term()}
  def parse_pub_ack(body) do
    cond do
      :binary.match(body, ~s("error")) != :nomatch ->
        case JSON.decode(body) do
          {:ok, %{"error" => error}} -> {:error, {:pub_ack, error}}
          _ -> {:error, :bad_pub_ack}
        end

      :binary.match(body, ~s("seq":)) == :nomatch ->
        {:error, :bad_pub_ack}

      :binary.match(body, ~s("duplicate":true)) != :nomatch ->
        Metrics.count("Nats/PublishDeduped")
        :ok

      true ->
        :ok
    end
  end
end
