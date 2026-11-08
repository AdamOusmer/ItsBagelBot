# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.AuthRpc do
  @moduledoc "Scoped native-auth RPC boundary; Users retains signing, sealing and grant custody."
  alias ControlsGateway.{Config, Nats}

  def token(request), do: request("token", request)
  def authorize(request), do: request("authorize", request)
  def revoke(request), do: request("revoke", request)

  defp request(verb, payload) do
    with {:ok, json} <- Jason.encode(payload),
         {:ok, %{body: body}} <-
           Nats.request(Config.auth_prefix() <> "." <> verb, json, 3_000),
         {:ok, reply} when is_map(reply) <- Jason.decode(body) do
      {:ok, reply}
    else
      _ -> {:error, :unavailable}
    end
  catch
    :exit, _ -> {:error, :unavailable}
  end
end
