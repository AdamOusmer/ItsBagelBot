defmodule Ingress.RpcTest do
  use ExUnit.Case, async: true

  test "builds generic and node-qualified subscription subjects" do
    assert Ingress.Rpc.subjects("bagel.rpc.health.ingress", "node2") == [
             "bagel.rpc.health.ingress",
             "bagel.rpc.health.ingress.node.node2"
           ]
  end

  test "keeps generic-only routing without a safe node token" do
    assert Ingress.Rpc.subjects("rpc.get", nil) == ["rpc.get"]
    assert Ingress.Rpc.subjects("rpc.get", "zone.node2") == ["rpc.get"]
    assert Ingress.Rpc.subjects("rpc.get", "node*") == ["rpc.get"]
  end
end
