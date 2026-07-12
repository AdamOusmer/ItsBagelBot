defmodule Ingress.JSON do
  @moduledoc """
  Native OTP JSON on the ingress firehose.

  OTP 27's `:json` encoder and decoder return/accept the exact map/list/binary
  shapes the Twitch and NATS paths use, while avoiding the protocol dispatch
  and intermediate allocation of the general-purpose Jason path.
  """

  @spec encode(term()) :: iodata()
  def encode(term), do: :json.encode(term, &encode_value/2)

  @spec decode(binary()) :: {:ok, term()} | {:error, term()}
  def decode(binary) when is_binary(binary) do
    case :json.decode(binary, :ok, %{null: nil}) do
      {term, :ok, ""} -> {:ok, term}
      {_term, :ok, rest} -> {:error, {:trailing_data, rest}}
    end
  catch
    kind, reason -> {:error, {kind, reason}}
  end

  # Elixir uses nil where Erlang's native JSON mapping uses the atom `null`.
  # Preserve Jason-compatible wire semantics for optional Twitch fields.
  defp encode_value(nil, _encode), do: "null"
  # Native :json has no protocol dispatch, so Elixir date/time structs would be
  # treated as plain maps and crash on their tuple `:microsecond` field
  # (:unsupported_type). Encode them as ISO 8601 strings, matching the
  # Jason.Encoder semantics the payloads relied on before the native-json
  # migration — e.g. status events carry `since: DateTime.utc_now()`.
  defp encode_value(%DateTime{} = value, encode), do: encode_value(DateTime.to_iso8601(value), encode)

  defp encode_value(%NaiveDateTime{} = value, encode),
    do: encode_value(NaiveDateTime.to_iso8601(value), encode)

  defp encode_value(%Date{} = value, encode), do: encode_value(Date.to_iso8601(value), encode)
  defp encode_value(%Time{} = value, encode), do: encode_value(Time.to_iso8601(value), encode)
  defp encode_value(value, encode), do: :json.encode_value(value, encode)
end
