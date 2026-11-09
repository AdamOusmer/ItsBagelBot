# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.RpcServer do
  alias Platform.Sift

  defmacro __using__(opts) do
    label = Keyword.fetch!(opts, :log)

    quote do
      use Platform.Nats.Server
      require Logger

      @impl true
      def error(_message, error) do
        Logger.error("#{unquote(label)} error: #{inspect(error)}")
        :ok
      end
    end
  end

  @spec decode_field(binary(), String.t(), (term() -> boolean())) ::
          {:ok, term()} | :invalid | {:error, String.t()}
  def decode_field(body, field, guard) do
    case Sift.decode(body) do
      {:ok, %{^field => value}} ->
        checked(value, guard)

      {:ok, _other} ->
        :invalid

      {:error, error} ->
        {:error, "json decode error: #{Exception.message(error)}"}
    end
  end

  defp checked(value, guard) do
    if guard.(value), do: {:ok, value}, else: :invalid
  end

  @spec scaler_reply({:error, term()}) :: %{error: String.t()}
  def scaler_reply({:error, message}) when is_binary(message), do: %{error: message}
  def scaler_reply({:error, :not_running}), do: %{error: "shard_scaler not running"}
  def scaler_reply({:error, reason}), do: %{error: inspect(reason)}
end
