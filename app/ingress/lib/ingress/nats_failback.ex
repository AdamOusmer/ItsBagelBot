# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary and unlicensed. See LICENSE.md.

defmodule Ingress.NatsFailback do
  @moduledoc """
  Returns displaced NATS connections to the same-node leaf after recovery.

  The ordinary `nats-leaf` Service remains available cluster-wide for failover;
  `nats-leaf-local` has `internalTrafficPolicy: Local` and is used only as proof
  that this node's leaf has recovered. Connections are moved one at a time.
  """

  use GenServer
  require Logger

  # Only the RPC plane (:gnat) is failed back to the node-local leaf. The BUS
  # plane (:gnat_bus) dials the hub directly (hub-direct firehose), whose
  # server_name is "nats-N", never "<node>--…", so local_connection?/2 would
  # always treat it as displaced and recycle it every 3 checks (~90s) forever.
  # It has no local-leaf to fail back to — leave it pinned to the hub.
  @connections [:gnat]

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    interval = positive_env("NATS_FAILBACK_INTERVAL_MS", 30_000)

    state = %{
      node: System.get_env("NODE_NAME", ""),
      health_url:
        System.get_env("NATS_LOCAL_LEAF_HEALTH_URL", "http://nats-leaf-local:8222/healthz"),
      interval: interval,
      required: positive_env("NATS_FAILBACK_SUCCESSES", 3),
      timeout: positive_env("NATS_FAILBACK_PROBE_TIMEOUT_MS", 1_000),
      successes: Map.new(@connections, &{&1, 0})
    }

    Process.send_after(self(), :check, :rand.uniform(interval))
    {:ok, state}
  end

  @impl true
  def handle_info(:check, %{node: ""} = state) do
    schedule(state)
  end

  def handle_info(:check, state) do
    local_ready = local_leaf_ready?(state.health_url, state.timeout)

    {successes, candidate} =
      Enum.reduce(@connections, {state.successes, nil}, fn name, {counts, candidate} ->
        locality = local_connection?(name, state.node)

        cond do
          locality == true ->
            # Already on the node-local leaf — nothing to do.
            {Map.put(counts, name, 0), candidate}

          locality == :unknown ->
            # Connection is reconnecting or between states (process absent,
            # server_info unavailable). Don't count this toward displacement;
            # leave the counter unchanged so a transient reconnect window
            # (e.g. after Gnat's own backoff) doesn't accumulate false
            # displacement evidence.
            {counts, candidate}

          !local_ready ->
            {Map.put(counts, name, 0), candidate}

          true ->
            # Connection is confirmed on a remote leaf and the local leaf is
            # healthy — count toward failback.
            count = Map.get(counts, name, 0) + 1
            next = if is_nil(candidate) and count >= state.required, do: name, else: candidate
            {Map.put(counts, name, count), next}
        end
      end)

    # One connection per check limits the blast radius for in-flight RPCs and
    # lets subscriptions settle before the other account is re-homed.
    successes =
      if candidate do
        server =
          try do
            Gnat.server_info(candidate).server_name
          catch
            :exit, _ -> "(unavailable)"
          end

        Logger.info("NatsFailback: recycling #{candidate} from #{server} back to node-local leaf")

        stop_connection(candidate)
        Map.put(successes, candidate, 0)
      else
        successes
      end

    schedule(%{state | successes: successes})
  end

  defp schedule(state) do
    Process.send_after(self(), :check, state.interval)
    {:noreply, state}
  end

  # Returns `true` when connected to the node-local leaf, `false` when
  # confirmed on a remote server, or `:unknown` when the connection process
  # is absent or between states (backoff reconnect window). The three-way
  # return lets the caller distinguish "definitely displaced" from "can't
  # tell yet", preventing transient reconnect windows from accumulating
  # false displacement evidence that triggers unnecessary connection kills.
  @spec local_connection?(atom(), String.t()) :: boolean() | :unknown
  defp local_connection?(name, node) do
    case Process.whereis(name) do
      nil ->
        :unknown

      _pid ->
        try do
          Gnat.server_info(name).server_name |> String.starts_with?(node <> "--")
        catch
          :exit, _ -> :unknown
        end
    end
  end

  defp stop_connection(name) do
    if Process.whereis(name) do
      try do
        Gnat.stop(name)
      catch
        :exit, _ -> :ok
      end
    end
  end

  @doc false
  def local_leaf_ready?(health_url, timeout) do
    with %URI{scheme: "http", host: host, port: port, path: path} when is_binary(host) <-
           URI.parse(health_url),
         {:ok, socket} <-
           :gen_tcp.connect(String.to_charlist(host), port, [:binary, active: false], timeout) do
      request = [
        "GET ",
        if(path in [nil, ""], do: "/healthz", else: path),
        " HTTP/1.1\r\nHost: ",
        host,
        "\r\nConnection: close\r\n\r\n"
      ]

      result =
        with :ok <- :gen_tcp.send(socket, request),
             {:ok, response} <- :gen_tcp.recv(socket, 0, timeout) do
          response
          |> :binary.split("\r\n", [:global])
          |> List.first()
          |> then(&(&1 in ["HTTP/1.1 200 OK", "HTTP/1.0 200 OK"]))
        else
          _ -> false
        end

      :gen_tcp.close(socket)
      result
    else
      _ -> false
    end
  end

  defp positive_env(key, fallback) do
    case Integer.parse(System.get_env(key, "")) do
      {value, ""} when value > 0 -> value
      _ -> fallback
    end
  end
end
