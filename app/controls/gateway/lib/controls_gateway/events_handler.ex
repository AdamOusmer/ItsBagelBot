# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.EventsHandler do
  @moduledoc "Cowboy loop handler for the SSE streams and their status check."
  @behaviour :cowboy_loop

  alias ControlsGateway.{Drain, Overlay, Tts}
  alias ControlsGateway.Overlays.Stream

  @retry_after "5"
  @base_headers %{"cache-control" => "no-store", "referrer-policy" => "no-referrer"}
  @kinds %{
    tts: %{session: Tts.Session, headers: @base_headers},
    overlay: %{
      session: Overlay.Session,
      headers: Map.put(@base_headers, "x-content-type-options", "nosniff")
    }
  }

  @impl true
  def init(req, name), do: route(:cowboy_req.method(req), name, Map.fetch!(@kinds, name), req)

  @impl true
  def info(message, req, state), do: Stream.info(message, req, state)

  defp route("GET", name, kind, req) do
    case Drain.track_connection(:stream) do
      :ok -> with_session(kind, req, &Stream.open(req, {name, version(req)}, &1, kind.headers))
      :draining -> reply(req, 503, Map.put(kind.headers, "retry-after", @retry_after))
    end
  end

  defp route("HEAD", name, kind, req) do
    if Drain.draining?(),
      do: reply(req, 503, Map.put(kind.headers, "retry-after", @retry_after)),
      else: with_session(kind, req, &head(req, Stream.status(name, &1), kind.headers))
  end

  defp route(_method, _name, kind, req),
    do: reply(req, 405, Map.put(kind.headers, "allow", "GET, HEAD"))

  defp with_session(kind, req, fun) do
    case kind.session.read(req) do
      {:ok, session} -> fun.(session)
      :error -> reply(req, 401, kind.headers)
    end
  end

  defp version(req) do
    case List.keyfind(:cowboy_req.parse_qs(req), "v", 0) do
      {"v", "2"} -> :v2
      _ -> :legacy
    end
  end

  defp head(req, {status, nil}, headers), do: reply(req, status, headers)

  defp head(req, {status, retry_after}, headers),
    do: reply(req, status, Map.put(headers, "retry-after", retry_after))

  defp reply(req, status, headers), do: {:ok, :cowboy_req.reply(status, headers, req), nil}
end
