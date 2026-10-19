# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialRpc do
  @moduledoc false
  require Logger
  alias Ingress.{JSON, Rpc, Trials}

  def reply_list do
    case Trials.list() do
      {:ok, result} ->
        result

      {:error, _} ->
        %{
          error: "unavailable",
          version: 0,
          active_count: 0,
          trials: []
        }
    end
  end

  def reply_add(body) do
    with {:ok, %{"broadcaster_id" => id}} <- JSON.decode(body),
         true <- Trials.valid_id?(id),
         {:ok, false} <- registered?(id),
         {:ok, result} <- Trials.add(id) do
      case registered?(id) do
        {:ok, false} ->
          if result == "duplicate",
            do: %{broadcaster_id: id, state: "existing"},
            else: %{broadcaster_id: id, state: "pending", enabled: true}

        {:ok, true} ->
          promote_registered(id)

        _ ->
          stop_unchecked(id)
          %{error: "unavailable"}
      end
    else
      false -> %{error: "invalid_id"}
      {:ok, true} -> %{error: "already_registered"}
      {:error, "full"} -> %{error: "full"}
      {:error, "invalid_id"} -> %{error: "invalid_id"}
      _ -> %{error: "unavailable"}
    end
  end

  def reply_set_enabled(body) do
    with {:ok, %{"broadcaster_id" => id, "enabled" => enabled}} <- JSON.decode(body),
         true <- Trials.valid_id?(id),
         true <- is_boolean(enabled),
         {:ok, false} <- if(enabled, do: registered?(id), else: {:ok, false}),
         {:ok, state} <- Trials.set_enabled(id, enabled) do
      %{broadcaster_id: id, enabled: enabled, state: state}
    else
      false -> %{error: "invalid_request"}
      {:ok, true} -> %{error: "already_registered"}
      {:error, "not_found"} -> %{error: "not_found"}
      {:error, "stopping"} -> %{error: "stopping"}
      _ -> %{error: "unavailable"}
    end
  end

  def reply_remove(body) do
    with {:ok, %{"broadcaster_id" => id}} <- JSON.decode(body),
         true <- Trials.valid_id?(id),
         {:ok, result} <- Trials.stop(id, "removed") do
      %{broadcaster_id: id, state: removed_state(result)}
    else
      false -> %{error: "invalid_id"}
      _ -> %{error: "unavailable"}
    end
  end

  defp stop_unchecked(id) do
    with {:error, reason} <- Trials.stop(id, "registration_check_unavailable") do
      Logger.warning("trial unchecked stop failed for #{id}: #{inspect(reason)}")
    end
  end

  defp promote_registered(id) do
    case Trials.stop(id, "promoted") do
      {:ok, _} ->
        %{error: "already_registered"}

      {:error, reason} ->
        Logger.warning("trial promotion failed for #{id}: #{inspect(reason)}")
        %{error: "unavailable"}
    end
  end

  defp removed_state(:absent), do: "absent"
  defp removed_state(_result), do: "stopping"

  def registered?(id) do
    request = JSON.encode(%{user_id: id})
    result = Rpc.request(:gnat, "bagel.rpc.internal.users.get", request, receive_timeout: 2_000)

    with {:ok, %{body: body}} <- result,
         {:ok, response} <- JSON.decode(body) do
      cond do
        is_map(response["user"]) -> {:ok, true}
        response["code"] == "not_found" -> {:ok, false}
        true -> {:error, :users_unavailable}
      end
    end
  catch
    :exit, _ -> {:error, :users_unavailable}
  end
end

defmodule Ingress.TrialListRpc do
  use Ingress.RpcServer, log: "trial list rpc"
  alias Ingress.{JSON, TrialRpc}
  @impl true
  def request(%{body: _}), do: {:reply, JSON.encode(TrialRpc.reply_list())}
end

defmodule Ingress.TrialAddRpc do
  use Ingress.RpcServer, log: "trial add rpc"
  alias Ingress.{JSON, TrialRpc}
  @impl true
  def request(%{body: body}), do: {:reply, JSON.encode(TrialRpc.reply_add(body))}
end

defmodule Ingress.TrialRemoveRpc do
  use Ingress.RpcServer, log: "trial remove rpc"
  alias Ingress.{JSON, TrialRpc}
  @impl true
  def request(%{body: body}), do: {:reply, JSON.encode(TrialRpc.reply_remove(body))}
end

defmodule Ingress.TrialSetEnabledRpc do
  use Ingress.RpcServer, log: "trial set enabled rpc"
  alias Ingress.{JSON, TrialRpc}
  @impl true
  def request(%{body: body}), do: {:reply, JSON.encode(TrialRpc.reply_set_enabled(body))}
end
