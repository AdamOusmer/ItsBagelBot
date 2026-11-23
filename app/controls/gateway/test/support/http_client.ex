# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.HttpClient do
  def open(port, method, path, opts \\ []) do
    {:ok, conn} =
      Mint.HTTP.connect(:http, "127.0.0.1", port, protocols: [:http1], mode: :passive)

    {:ok, conn, _ref} =
      Mint.HTTP.request(conn, method, path, Keyword.get(opts, :headers, []), opts[:body])

    await(
      %{conn: conn, status: nil, headers: %{}, body: "", done: false},
      &(map_size(&1.headers) > 0 or &1.done)
    )
  end

  def await(client, match?) do
    if match?.(client) do
      client
    else
      {:ok, conn, frames} = Mint.HTTP.recv(client.conn, 0, 3_000)
      client = Enum.reduce(frames, %{client | conn: conn}, &apply_frame/2)
      await(client, match?)
    end
  end

  def request(port, method, path, opts \\ []) do
    response = port |> open(method, path, opts) |> await(& &1.done)
    close(response)
    response
  end

  def drain(client, timeout_ms) do
    case Mint.HTTP.recv(client.conn, 0, timeout_ms) do
      {:ok, conn, frames} ->
        drain(Enum.reduce(frames, %{client | conn: conn}, &apply_frame/2), timeout_ms)

      {:error, conn, _reason, frames} ->
        Enum.reduce(frames, %{client | conn: conn}, &apply_frame/2)
    end
  end

  def alerts(client) do
    ~r/event: alert\ndata: (.*)\n\n/
    |> Regex.scan(client.body, capture: :all_but_first)
    |> Enum.map(fn [json] -> Jason.decode!(json) end)
  end

  def ready(client) do
    case Regex.run(~r/event: ready\ndata: (.*)\n\n/, client.body, capture: :all_but_first) do
      [json] -> Jason.decode!(json)
      nil -> nil
    end
  end

  def await_alerts(client, count), do: await(client, &(length(alerts(&1)) >= count))

  def close(client), do: Mint.HTTP.close(client.conn)

  defp apply_frame({:status, _ref, status}, client), do: %{client | status: status}
  defp apply_frame({:headers, _ref, headers}, client), do: %{client | headers: Map.new(headers)}
  defp apply_frame({:data, _ref, data}, client), do: %{client | body: client.body <> data}
  defp apply_frame({:done, _ref}, client), do: %{client | done: true}
end
