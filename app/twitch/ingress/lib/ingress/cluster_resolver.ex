# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ClusterResolver do
  @key {__MODULE__, :addresses}

  def resolve(name, inner \\ &default_lookup/1) do
    handle(inner.(name), name)
  end

  defp handle(
         {:ok, {:hostent, _fqdn, _aliases, :inet, _len, [_ | _] = addresses}} = result,
         _name
       ) do
    remember(addresses)
    result
  end

  defp handle({:ok, {:hostent, _fqdn, _aliases, :inet, _len, []}} = result, _name), do: result

  defp handle({:error, _reason} = result, name), do: substitute(result, name)

  defp handle(result, _name), do: result

  defp substitute(error_result, name) do
    case :persistent_term.get(@key, :none) do
      :none -> error_result
      addresses -> {:ok, {:hostent, name, [], :inet, 4, addresses}}
    end
  end

  defp remember(addresses) do
    if :persistent_term.get(@key, :none) != addresses do
      :persistent_term.put(@key, addresses)
    end
  end

  defp default_lookup(name), do: :inet_res.getbyname(name, :a)
end
