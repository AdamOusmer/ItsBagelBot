# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialRpc do
  @moduledoc false
  alias Ingress.{JSON, Rpc, Trials}

  def reply_list do
    case Trials.list() do
      {:ok, result} ->
        Map.put(result, :admission_enabled, admission_enabled?())

      {:error, _} ->
        %{
          error: "unavailable",
          version: 0,
          active_count: 0,
          admission_enabled: admission_enabled?(),
          trials: []
        }
    end
  end

  def reply_add(body) do
    if admission_enabled?() do
      with {:ok, %{"broadcaster_id" => id}} <- JSON.decode(body),
           true <- Trials.valid_id?(id),
           {:ok, false} <- registered?(id),
           {:ok, _generation} <- Trials.add(id) do
        case registered?(id) do
          {:ok, false} ->
            %{broadcaster_id: id, state: "pending"}

          {:ok, true} ->
            Trials.stop(id)
            Trials.field(id, "stop_reason", "promoted")
            %{error: "already_registered"}

          _ ->
            Trials.stop(id)
            Trials.field(id, "stop_reason", "registration_check_unavailable")
            %{error: "unavailable"}
        end
      else
        false -> %{error: "invalid_id"}
        {:ok, true} -> %{error: "already_registered"}
        {:error, "full"} -> %{error: "full"}
        {:error, "invalid_id"} -> %{error: "invalid_id"}
        _ -> %{error: "unavailable"}
      end
    else
      %{error: "admission_disabled"}
    end
  end

  defp admission_enabled?, do: System.get_env("TRIAL_ADMISSION_ENABLED", "off") == "on"

  def reply_remove(body) do
    with {:ok, %{"broadcaster_id" => id}} <- JSON.decode(body),
         true <- Trials.valid_id?(id),
         {:ok, state} <- Trials.stop(id) do
      if state == "stopping", do: Trials.field(id, "stop_reason", "removed")
      %{broadcaster_id: id, state: state}
    else
      false -> %{error: "invalid_id"}
      _ -> %{error: "unavailable"}
    end
  end

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
