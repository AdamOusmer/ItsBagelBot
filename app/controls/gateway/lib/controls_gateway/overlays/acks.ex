# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Acks do
  alias ControlsGateway.{Http, Metrics, OverlayRequest}
  alias ControlsGateway.Overlays.Handle
  alias Platform.Nats.Ack

  @max_body 8_192
  @max_handles 32
  @ack_metrics %{ack: "Ack/Ack", progress: "Ack/Progress"}
  @reasons ["played", "duplicate", "expired", "dropped"]
  @reason_metrics %{
    "duplicate" => "Page/DuplicateSkipped",
    "expired" => "Page/Expired",
    "dropped" => "Page/Dropped"
  }

  def ack(conn, session, headers), do: run(conn, session, headers, :ack)
  def progress(conn, session, headers), do: run(conn, session, headers, :progress)

  defp run(conn, session, headers, action) do
    {json, conn} = read_json(conn)
    status = status(action, request(action, json), session.user_id)
    Http.reply(conn, status, headers)
  end

  defp read_json(conn) do
    if OverlayRequest.json?(conn),
      do: decode(OverlayRequest.read_body(conn, @max_body)),
      else: {:error, conn}
  end

  defp decode({{:ok, body}, conn}) do
    case Jason.decode(body) do
      {:ok, %{} = json} -> {json, conn}
      _ -> {:error, conn}
    end
  end

  defp decode({{:error, _reason}, conn}), do: {:error, conn}

  defp request(:ack, %{"handle" => handle} = json) do
    case Map.fetch(json, "reason") do
      :error -> {[handle], nil}
      {:ok, reason} when reason in @reasons -> {[handle], reason}
      {:ok, _reason} -> :error
    end
  end

  defp request(:progress, %{"handles" => handles})
       when is_list(handles) and length(handles) <= @max_handles,
       do: {handles, nil}

  defp request(_action, _json), do: :error

  defp status(_action, :error, _user_id), do: 404

  defp status(action, {handles, reason}, user_id) do
    results = Enum.map(handles, &Handle.verify(user_id, &1))

    if Enum.all?(results, &match?({:ok, _}, &1)) do
      publish(action, Enum.map(results, &elem(&1, 1)), reason)
    else
      404
    end
  end

  defp publish(action, subjects, reason) do
    if subjects |> Enum.map(&send_ack(action, &1)) |> Enum.all?(&(&1 == :ok)) do
      Metrics.count(Map.fetch!(@ack_metrics, action), length(subjects))
      count_reason(reason)
      204
    else
      Metrics.count("Ack/Failed")
      503
    end
  end

  defp count_reason(reason) do
    case Map.fetch(@reason_metrics, reason) do
      {:ok, metric} -> Metrics.count(metric)
      :error -> :ok
    end
  end

  defp send_ack(:ack, subject), do: Ack.ack(:jetstream, subject)
  defp send_ack(:progress, subject), do: Ack.progress(:jetstream, subject)
end
