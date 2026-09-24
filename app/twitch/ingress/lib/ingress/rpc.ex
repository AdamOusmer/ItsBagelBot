# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Rpc do
  @node_token "node"

  def subjects(subject, node \\ System.get_env("NODE_NAME")) do
    case local_subject(subject, node) do
      nil -> [subject]
      local -> [subject, local]
    end
  end

  def request(connection, subject, body, opts \\ []) do
    case subjects(subject) do
      [generic] ->
        Gnat.request(connection, generic, body, opts)

      [generic, local] ->
        # Only :no_responders proves nothing ran; retrying a timeout can repeat a mutation.
        case Gnat.request(connection, local, body, opts) do
          {:error, :no_responders} -> Gnat.request(connection, generic, body, opts)
          result -> result
        end
    end
  end

  defp local_subject(_subject, node) when not is_binary(node) or node == "", do: nil

  defp local_subject(subject, node) do
    if Regex.match?(~r/^[^.*>\s]+$/u, node) do
      Enum.join([subject, @node_token, node], ".")
    end
  end
end
