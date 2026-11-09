# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.WebSocket.Frame do
  @moduledoc false

  @invalid_close_codes [1_004, 1_005, 1_006, 1_016, 1_100, 2_000, 2_999]

  defstruct buffer: "", fragment: nil

  def decode(%__MODULE__{buffer: "", fragment: fragment}, data), do: frames(data, fragment, [])

  def decode(%__MODULE__{buffer: buffer, fragment: fragment}, data),
    do: frames(buffer <> data, fragment, [])

  defp frames(
         <<fin::1, rsv::3, opcode::4, masked::1, len::7, rest::binary>> = data,
         fragment,
         acc
       ) do
    with :ok <- check_header(rsv, masked),
         {:ok, size, rest} <- payload_size(len, rest),
         <<payload::binary-size(^size), rest::binary>> <- rest,
         {:ok, frame, fragment} <- frame(opcode, fin == 1, payload, fragment) do
      frames(rest, fragment, if(frame, do: [frame | acc], else: acc))
    else
      {:error, reason} -> {:error, reason}
      _incomplete -> {:ok, %__MODULE__{buffer: data, fragment: fragment}, Enum.reverse(acc)}
    end
  end

  defp frames(data, fragment, acc),
    do: {:ok, %__MODULE__{buffer: data, fragment: fragment}, Enum.reverse(acc)}

  defp check_header(0, 0), do: :ok
  defp check_header(_rsv, 1), do: {:error, :masked_server_frame}
  defp check_header(_rsv, _masked), do: {:error, :reserved_bits}

  defp payload_size(126, <<size::16, rest::binary>>), do: {:ok, size, rest}
  defp payload_size(127, <<0::1, size::63, rest::binary>>), do: {:ok, size, rest}
  defp payload_size(127, <<1::1, _::63, _::binary>>), do: {:error, :malformed_payload_length}
  defp payload_size(len, rest) when len < 126, do: {:ok, len, rest}
  defp payload_size(_len, _rest), do: :incomplete

  defp frame(opcode, true, payload, nil) when opcode in [1, 2], do: complete(opcode, payload, nil)

  defp frame(opcode, false, payload, nil) when opcode in [1, 2],
    do: {:ok, nil, {opcode, [payload]}}

  defp frame(opcode, _fin, _payload, _fragment) when opcode in [1, 2],
    do: {:error, :interleaved_message}

  defp frame(0, _fin, _payload, nil), do: {:error, :unexpected_continuation}
  defp frame(0, false, payload, {opcode, parts}), do: {:ok, nil, {opcode, [payload | parts]}}

  defp frame(0, true, payload, {opcode, parts}),
    do: complete(opcode, IO.iodata_to_binary(Enum.reverse([payload | parts])), nil)

  defp frame(opcode, false, _payload, _fragment) when opcode in 8..10,
    do: {:error, :fragmented_control_frame}

  defp frame(opcode, true, payload, _fragment) when opcode in 8..10 and byte_size(payload) > 125,
    do: {:error, :oversized_control_frame}

  defp frame(8, true, payload, fragment), do: close(payload, fragment)
  defp frame(9, true, payload, fragment), do: {:ok, {:ping, payload}, fragment}
  defp frame(10, true, payload, fragment), do: {:ok, {:pong, payload}, fragment}
  defp frame(opcode, _fin, _payload, _fragment), do: {:error, {:unsupported_opcode, opcode}}

  defp complete(1, payload, fragment) do
    if String.valid?(payload, :fast_ascii),
      do: {:ok, {:text, payload}, fragment},
      else: {:error, :invalid_utf8}
  end

  defp complete(2, payload, fragment), do: {:ok, {:binary, payload}, fragment}

  defp close(<<>>, fragment), do: {:ok, {:close, 1_000, ""}, fragment}

  defp close(<<code::16, reason::binary>>, fragment)
       when code in 1_000..4_999 and code not in @invalid_close_codes do
    if String.valid?(reason, :fast_ascii),
      do: {:ok, {:close, code, reason}, fragment},
      else: {:error, :invalid_close_payload}
  end

  defp close(_payload, _fragment), do: {:error, :invalid_close_payload}
end
