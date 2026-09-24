# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.JSON do
  alias Ingress.LaneMessage

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

  @spec members(map()) :: iodata()
  def members(map) do
    map
    |> Enum.map(fn {key, value} -> [encode(to_string(key)), ?:, encode(value)] end)
    |> Enum.intersperse(?,)
  end

  defp encode_value(nil, _encode), do: "null"

  defp encode_value(%LaneMessage{lane: lane, body: body}, encode),
    do: [~s({"lane":), encode_value(Atom.to_string(lane), encode), ?,, body, ?}]

  defp encode_value(%DateTime{} = value, encode),
    do: encode_value(DateTime.to_iso8601(value), encode)

  defp encode_value(%NaiveDateTime{} = value, encode),
    do: encode_value(NaiveDateTime.to_iso8601(value), encode)

  defp encode_value(%Date{} = value, encode), do: encode_value(Date.to_iso8601(value), encode)
  defp encode_value(%Time{} = value, encode), do: encode_value(Time.to_iso8601(value), encode)
  defp encode_value(value, encode), do: :json.encode_value(value, encode)
end
