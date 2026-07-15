defmodule Ingress.HealthRpcTest do
  use ExUnit.Case, async: true

  test "returns the standard side-effect-free health envelope" do
    assert {:reply, body} = Ingress.HealthRpc.request(%{body: "{}"})
    assert Jason.decode!(body) == %{"service" => "ingress", "ok" => true}
  end
end
