defmodule Ingress.Rpc do
  @moduledoc """
  Node-local-first Core NATS routing with the generic queue retained for HA.

  A generic retry is safe only after NATS returns `:no_responders`. Timeouts and
  connection failures are returned unchanged because a mutation may have run.
  """

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
