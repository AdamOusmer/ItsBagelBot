# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.RpcServer do
  @moduledoc """
  Template Method for this service's NATS request/reply handlers.

  `use Ingress.RpcServer, log: "scale rpc"` fixes the whole skeleton — the
  `Gnat.Server` behaviour and the `error/2` callback, which logs under the
  given label and answers `:ok` so a handler crash never takes the consumer
  down with it. `request/1` is the only hook a handler still writes.

  The callback was byte-identical in six modules but for that label, and
  `Gnat.Server`'s own default cannot be used instead: it logs the same line for
  every handler, so a fleet alert could not say which subject failed.

  `decode_field/3` and `scaler_reply/1` are the shared halves of the two
  scaler-mutating handlers, whose `request/1` was duplicated whole. The usage
  string and the scaler call stay at the call site: they are the only things
  that actually differ, and inlining them there keeps each handler's contract
  readable in one screen.

  Those two are imported by the handlers that use them, not by `__using__`.
  Injecting them made four of the six handlers carry names they never call, and
  an import a reader cannot see at the call site is exactly the thing `use`
  should not be smuggling in.
  """

  alias Ingress.JSON

  defmacro __using__(opts) do
    label = Keyword.fetch!(opts, :log)

    quote do
      use Gnat.Server
      require Logger

      @impl Gnat.Server
      def error(_message, error) do
        Logger.error("#{unquote(label)} error: #{inspect(error)}")
        :ok
      end
    end
  end

  @doc """
  Decodes a JSON request body and pulls `field` out of it, checking the value
  with `guard`.

  Returns `{:ok, value}`, `:invalid` for a body that decoded but does not carry
  an acceptable `field` (the caller owns the usage string, which names its own
  field), or `{:error, reason}` for a decode failure.
  """
  @spec decode_field(binary(), String.t(), (term() -> boolean())) ::
          {:ok, term()} | :invalid | {:error, String.t()}
  def decode_field(body, field, guard) do
    case JSON.decode(body) do
      {:ok, %{^field => value}} ->
        checked(value, guard)

      {:ok, _other} ->
        :invalid

      # `Ingress.JSON.decode/1` reports the caught kind/reason (or trailing
      # data) as a pair, which is what separates a decode failure from the
      # handler's own atom errors.
      {:error, {_kind, _reason} = decode_error} ->
        {:error, "json decode error: #{inspect(decode_error)}"}
    end
  end

  defp checked(value, guard) do
    if guard.(value), do: {:ok, value}, else: :invalid
  end

  @doc """
  Renders an `Ingress.ShardScaler` failure (or a `decode_field/3` error) as the
  `{"error": "..."}` document both scaler handlers reply with.
  """
  @spec scaler_reply({:error, term()}) :: %{error: String.t()}
  def scaler_reply({:error, message}) when is_binary(message), do: %{error: message}
  def scaler_reply({:error, :not_running}), do: %{error: "shard_scaler not running"}
  def scaler_reply({:error, reason}), do: %{error: inspect(reason)}
end
