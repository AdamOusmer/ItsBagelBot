defmodule Ingress.ClusterResolverTest do
  # async: false — the module keeps its last-known-good set in global
  # persistent_term, so tests must not run concurrently and each clears the slot.
  use ExUnit.Case, async: false

  alias Ingress.ClusterResolver

  @service ~c"twitch-ingress-headless"
  @key {Ingress.ClusterResolver, :addresses}

  defp hostent(addresses), do: {:ok, {:hostent, @service, [], :inet, 4, addresses}}

  setup do
    :persistent_term.erase(@key)
    on_exit(fn -> :persistent_term.erase(@key) end)
    :ok
  end

  test "successful non-empty lookup passes through unchanged and is remembered" do
    good = hostent([{10, 0, 0, 1}, {10, 0, 0, 2}])
    assert ^good = ClusterResolver.resolve(@service, fn _ -> good end)

    # Remembered: a later error replays the same addresses as a synthetic success.
    assert {:ok, {:hostent, _fqdn, [], :inet, _len, [{10, 0, 0, 1}, {10, 0, 0, 2}]}} =
             ClusterResolver.resolve(@service, fn _ -> {:error, :timeout} end)
  end

  test "successful empty lookup is honored (genuine shrink) and not cached" do
    empty = hostent([])
    assert ^empty = ClusterResolver.resolve(@service, fn _ -> empty end)

    # Nothing cached, so a following error stays an error (nothing to resurrect).
    assert {:error, :nxdomain} =
             ClusterResolver.resolve(@service, fn _ -> {:error, :nxdomain} end)
  end

  test "error with a prior good set substitutes the last known addresses" do
    _ = ClusterResolver.resolve(@service, fn _ -> hostent([{10, 0, 0, 5}]) end)

    result = ClusterResolver.resolve(@service, fn _ -> {:error, :timeout} end)

    # The substituted tuple satisfies the dep's own success match, and carries
    # exactly the remembered addresses (the load-bearing shape claim).
    assert {:ok, {:hostent, _fqdn, [], :inet, _len, addresses}} = result
    assert addresses == [{10, 0, 0, 5}]
  end

  test "error with no prior good returns the error unchanged (first boot)" do
    assert {:error, :nxdomain} =
             ClusterResolver.resolve(@service, fn _ -> {:error, :nxdomain} end)
  end

  test "a fresh non-empty success overwrites the remembered set (post scale-down)" do
    _ =
      ClusterResolver.resolve(@service, fn _ ->
        hostent([{10, 0, 0, 1}, {10, 0, 0, 2}, {10, 0, 0, 3}])
      end)

    _ = ClusterResolver.resolve(@service, fn _ -> hostent([{10, 0, 0, 1}, {10, 0, 0, 2}]) end)

    # After the shrink was observed, an error replays only the surviving two:
    # the departed node is never resurrected.
    assert {:ok, {:hostent, _fqdn, [], :inet, _len, [{10, 0, 0, 1}, {10, 0, 0, 2}]}} =
             ClusterResolver.resolve(@service, fn _ -> {:error, :timeout} end)
  end
end
