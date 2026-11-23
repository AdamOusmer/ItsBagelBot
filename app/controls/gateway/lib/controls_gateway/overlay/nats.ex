# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Nats do
  alias ControlsGateway.OverlayRequest
  alias ControlsGateway.Tts.Session
  alias Platform.Cache
  alias Platform.Nats.Planes

  @resolve "rpc.internal.projector.overlays.resolve"
  @cache :overlay_resolve

  def resolve(hash) do
    case Cache.get_or_load(@cache, hash, fn -> fetch(hash) end) do
      {:ok, :not_found} -> {:not_found, :unknown_token}
      {:ok, overlay} -> {:ok, overlay}
      {:error, reason} -> {:unavailable, reason}
    end
  end

  defp fetch(hash) do
    case OverlayRequest.request(&Planes.request/1, @resolve, %{token_hash: hash}) do
      {:ok, %{"code" => "not_found"}} -> {:ok, :not_found}
      {:ok, %{"code" => code} = reply} when code not in [nil, ""] -> unavailable(reply)
      {:ok, reply} -> overlay(reply)
      other -> unavailable(other)
    end
  end

  def check(session) do
    case resolve(session.token_hash) do
      {:ok, overlay} -> verdict(overlay, same?(overlay, session))
      {:not_found, _reason} -> {:denied, :not_found}
      {:unavailable, _reason} -> :unavailable
    end
  end

  defp same?(overlay, session) do
    overlay.overlay_id == session.overlay_id and overlay.generation == session.generation
  end

  defp verdict(_overlay, false), do: {:denied, :not_found}
  defp verdict(%{allowed: false}, true), do: {:denied, :premium_required}
  defp verdict(overlay, true), do: {:ok, overlay}

  defp overlay(reply) do
    with %{
           "user_id" => user_id,
           "overlay_id" => id,
           "generation" => generation,
           "revision" => revision,
           "allowed" => allowed,
           "premium" => premium
         } <- reply,
         true <- Session.valid_user_id?(user_id),
         true <- Session.valid_identity?(id, generation),
         true <- is_integer(revision),
         true <- Enum.all?([allowed, premium], &is_boolean/1),
         {:ok, document} <- document(allowed, reply["document"]) do
      {:ok,
       %{
         user_id: user_id,
         overlay_id: id,
         generation: generation,
         revision: revision,
         allowed: allowed,
         document: document,
         premium: premium
       }}
    else
      _ -> {:error, :invalid_reply}
    end
  end

  defp document(true, %{} = document), do: {:ok, document}
  defp document(false, _document), do: {:ok, nil}
  defp document(_allowed, _document), do: :error

  defp unavailable(reason), do: {:error, inspect(reason, limit: 5)}
end
