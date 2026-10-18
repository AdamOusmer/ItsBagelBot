# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialUserChanged do
  use Ingress.RpcServer, log: "trial user changed"
  alias Ingress.{JSON, Trials}

  @impl true
  def request(%{body: body}) do
    with {:ok, %{"user_id" => id}} <- JSON.decode(body),
         id = to_string(id),
         true <- Trials.valid_id?(id),
         {:error, reason} <- Trials.stop(id, "promoted") do
      Logger.warning("trial promotion failed for #{id}: #{inspect(reason)}")
    end

    :ok
  end
end
