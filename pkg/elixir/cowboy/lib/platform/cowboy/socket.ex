# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Cowboy.Socket do
  @moduledoc false
  @behaviour :cowboy_websocket

  @impl true
  def init(req, socket), do: {:cowboy_websocket, req, socket}

  @impl true
  def websocket_init(socket), do: result(socket, socket.handler.init(socket.state))

  @impl true
  def websocket_handle({opcode, data}, socket) when opcode in [:text, :binary] do
    now = System.monotonic_time(:second)
    count = if socket.second == now, do: socket.count + 1, else: 1
    socket = %{socket | second: now, count: count}

    cond do
      byte_size(data) > socket.frame_limit -> stop(socket, 1009, "message too large")
      count > socket.rate -> stop(socket, 1008, "too many messages")
      overloaded?(socket) -> stop(socket, 1013, "overloaded")
      true -> result(socket, socket.handler.handle_in({opcode, data}, socket.state))
    end
  end

  def websocket_handle(_control, socket), do: {:ok, socket, :hibernate}

  @impl true
  def websocket_info(message, socket) do
    if overloaded?(socket),
      do: stop(socket, 1013, "overloaded"),
      else: result(socket, socket.handler.handle_info(message, socket.state))
  end

  @impl true
  def terminate(reason, _req, socket), do: socket.handler.terminate(reason, socket.state)

  defp overloaded?(socket) do
    {:message_queue_len, length} = Process.info(self(), :message_queue_len)
    length > socket.mailbox
  end

  defp result(socket, {:ok, state}), do: {:ok, %{socket | state: state}}
  defp result(socket, {:ok, state, :hibernate}), do: {:ok, %{socket | state: state}, :hibernate}
  defp result(socket, {:push, frames, state}), do: push(%{socket | state: state}, frames, false)

  defp result(socket, {:push, frames, state, :hibernate}),
    do: push(%{socket | state: state}, frames, true)

  defp result(socket, {:stop, code, reason, state}),
    do: checked_stop(%{socket | state: state}, code, reason)

  defp result(socket, {:stop, code, reason, state, :hibernate}),
    do: checked_stop(%{socket | state: state}, code, reason)

  defp result(socket, _), do: stop(socket, 1011, "invalid callback result")

  defp push(socket, frames, hibernate) do
    case bounded_frames(frames) do
      {:ok, frames} -> push_checked(socket, frames, hibernate)
      :oversize -> stop(socket, 1009, "outbound limit")
      :invalid -> stop(socket, 1011, "invalid callback result")
    end
  end

  defp bounded_frames([_ | _] = frames), do: check_list(frames, 0, frames)
  defp bounded_frames([]), do: {:ok, []}
  defp bounded_frames(frame), do: {:ok, [frame]}

  defp check_list([], _, original), do: {:ok, original}
  defp check_list([_ | _], 16, _), do: :oversize
  defp check_list([_ | rest], count, original), do: check_list(rest, count + 1, original)
  defp check_list(_, _, _), do: :invalid

  defp push_checked(socket, frames, hibernate) do
    cond do
      Enum.any?(frames, &oversize?(&1, socket.frame_limit)) ->
        stop(socket, 1009, "outbound limit")

      not Enum.all?(frames, &valid_frame?/1) ->
        stop(socket, 1011, "invalid callback result")

      hibernate ->
        {:reply, frames, socket, :hibernate}

      true ->
        {:reply, frames, socket}
    end
  end

  defp oversize?({type, data}, limit)
       when type in [:text, :binary, :ping, :pong] and is_binary(data) do
    byte_size(data) > limit or (type in [:ping, :pong] and byte_size(data) > 125)
  end

  defp oversize?(_, _), do: false
  defp valid_frame?({:text, data}) when is_binary(data), do: String.valid?(data)

  defp valid_frame?({type, data}) when type in [:binary, :ping, :pong] and is_binary(data),
    do: true

  defp valid_frame?(_), do: false

  defp checked_stop(socket, code, reason) do
    if valid_close_code?(code) and is_binary(reason) and byte_size(reason) <= 123 and
         String.valid?(reason),
       do: stop(socket, code, reason),
       else: stop(socket, 1011, "invalid callback result")
  end

  defp valid_close_code?(code)
       when code in [1000, 1001, 1002, 1003, 1007, 1008, 1009, 1010, 1011, 1012, 1013, 1014],
       do: true

  defp valid_close_code?(code) when is_integer(code) and code in 3000..4999, do: true
  defp valid_close_code?(_), do: false
  defp stop(socket, code, reason), do: {:reply, {:close, code, reason}, socket}
end
