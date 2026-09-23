# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialUserChanged do
  use Ingress.RpcServer, log: "trial user changed"
  alias Ingress.{JSON, TrialMembership, Trials}

  @impl true
  def request(%{body: body}) do
    with {:ok, %{"user_id" => id}} <- JSON.decode(body),
         id when is_binary(id) <- to_string(id),
         %{state: state} <- TrialMembership.lookup(id),
         true <- state != "stopping" do
      Trials.stop(id)
      Trials.field(id, "stop_reason", "promoted")
    else
      _ -> :ok
    end

    :ok
  end
end
