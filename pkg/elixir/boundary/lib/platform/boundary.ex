# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Boundary do
  @moduledoc "Finds direct vendor references that must go through the shared Platform packages."

  @aliases [
    [:Gnat],
    [:Jason],
    [:Torque],
    [:Poison],
    [:Jiffy],
    [:Thoas],
    [:JSON],
    [:Mint],
    [:Redix],
    [:NewRelic],
    [:Protox],
    [:Cowboy],
    [:Ranch],
    [:WebSock],
    [:WebSockAdapter],
    [:Plug, :Cowboy]
  ]
  @alias_names Enum.map(@aliases, &("Elixir." <> Enum.join(&1, ".")))
  @called_modules [
    :gnat,
    :torque,
    :redix,
    :mint,
    :protox,
    :new_relic,
    :new_relic_agent,
    :json,
    :jiffy,
    :thoas
  ]
  @module_families ["cowboy", "cowlib", "cow", "ranch"]

  def violations(paths) do
    for path <- paths,
        source = File.read!(path),
        not String.starts_with?(source, "# Code generated"),
        reference <- references(source),
        do: {path, reference}
  end

  def references(source) do
    source
    |> Code.string_to_quoted!()
    |> Macro.prewalk([], &collect/2)
    |> elem(1)
    |> Enum.reverse()
  end

  defp collect({:__aliases__, _, parts} = node, acc),
    do: {node, if(vendor_alias?(parts), do: [Module.concat(parts) | acc], else: acc)}

  defp collect({{:., _, [module, _]}, _, _} = node, acc) when is_atom(module),
    do: {node, if(called_module?(module), do: [module | acc], else: acc)}

  defp collect({:apply, _, [module, _, _]} = node, acc) when is_atom(module),
    do: {node, if(called_module?(module), do: [module | acc], else: acc)}

  defp collect(atom, acc) when is_atom(atom),
    do: {atom, if(family?(atom), do: [atom | acc], else: acc)}

  defp collect(node, acc), do: {node, acc}

  defp vendor_alias?(parts), do: Enum.any?(@aliases, &List.starts_with?(parts, &1))

  defp called_module?(module) do
    name = Atom.to_string(module)

    module in @called_modules or family?(module) or
      Enum.any?(@alias_names, &(name == &1 or String.starts_with?(name, &1 <> ".")))
  end

  defp family?(atom) do
    name = Atom.to_string(atom)
    Enum.any?(@module_families, &(name == &1 or String.starts_with?(name, &1 <> "_")))
  end
end
