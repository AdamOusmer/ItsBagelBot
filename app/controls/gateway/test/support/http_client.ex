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

  def close(client), do: Mint.HTTP.close(client.conn)

  defp apply_frame({:status, _ref, status}, client), do: %{client | status: status}
  defp apply_frame({:headers, _ref, headers}, client), do: %{client | headers: Map.new(headers)}
  defp apply_frame({:data, _ref, data}, client), do: %{client | body: client.body <> data}
  defp apply_frame({:done, _ref}, client), do: %{client | done: true}
end
