# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Acks do
  alias ControlsGateway.Overlays.{Ack, Handle}

  @max_body 8_192
  @max_handles 32

  def ack(req, session, headers), do: run(req, session, headers, :ack)
  def progress(req, session, headers), do: run(req, session, headers, :progress)

  defp run(req, session, headers, action) do
    {json, req} = read_json(req)
    status = status(action, handles(action, json), session.user_id)
    {:ok, :cowboy_req.reply(status, headers, req), nil}
  end

  defp read_json(req) do
    with {"application", "json", _params} <- :cowboy_req.parse_header("content-type", req),
         {:ok, body, req} <- :cowboy_req.read_body(req, %{length: @max_body, period: 2_000}),
         {:ok, %{} = json} <- Jason.decode(body) do
      {json, req}
    else
      {:more, _body, req} -> {:error, req}
      _ -> {:error, req}
    end
  catch
    :exit, {:request_error, _, _} -> {:error, req}
  end

  defp handles(:ack, %{"handle" => handle}), do: [handle]

  defp handles(:progress, %{"handles" => handles})
       when is_list(handles) and length(handles) <= @max_handles,
       do: handles

  defp handles(_action, _json), do: :error

  defp status(_action, :error, _user_id), do: 404

  defp status(action, handles, user_id) do
    results = Enum.map(handles, &Handle.verify(user_id, &1))

    if Enum.all?(results, &match?({:ok, _}, &1)) do
      publish(action, Enum.map(results, &elem(&1, 1)))
    else
      404
    end
  end

  defp publish(action, subjects) do
    if subjects |> Enum.map(&send_ack(action, &1)) |> Enum.all?(&(&1 == :ok)), do: 204, else: 503
  end

  defp send_ack(:ack, subject), do: Ack.ack(subject)
  defp send_ack(:progress, subject), do: Ack.progress(subject)
end
