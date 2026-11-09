# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Cowboy.Server do
  @moduledoc "Cowboy listener and bounded WebSocket upgrade."

  @listener_options [
    :name,
    :port,
    :plug,
    :scheme,
    :certfile,
    :keyfile,
    :ip,
    :num_acceptors,
    :max_connections
  ]
  @upgrade_options [
    :max_frame_bytes,
    :max_messages_per_second,
    :max_mailbox_messages,
    :idle_timeout
  ]

  defguardp int_in(value, min, max) when is_integer(value) and value >= min and value <= max
  defguardp non_empty(value) when is_binary(value) and byte_size(value) > 0

  def child_spec(opts) do
    validate_options!(opts, @listener_options)
    plug = Keyword.fetch!(opts, :plug)
    name = Keyword.fetch!(opts, :name)
    port = Keyword.get(opts, :port, 0)
    check!(is_atom(name) and int_in(port, 0, 65_535), "invalid listener name or port")
    check!(valid_plug?(plug), "invalid plug")

    scheme = Keyword.get(opts, :scheme, :http)
    acceptors = Keyword.get(opts, :num_acceptors, 2)
    ip = Keyword.get(opts, :ip, {0, 0, 0, 0})
    max_connections = Keyword.get(opts, :max_connections, 16_384)

    check!(
      int_in(acceptors, 1, 128) and int_in(max_connections, 1, 1_000_000) and valid_ip?(ip),
      "invalid listener capacity, acceptor count or IP address"
    )

    tls = tls(scheme, Keyword.get(opts, :certfile), Keyword.get(opts, :keyfile))

    Plug.Cowboy.child_spec(
      [
        scheme: scheme,
        plug: plug,
        ref: name,
        port: port,
        ip: ip,
        transport_options: [num_acceptors: acceptors, max_connections: max_connections],
        protocol_options: [protocols: [:http]]
      ] ++ tls
    )
  end

  def port(name), do: :ranch.get_port(name)

  def upgrade(conn, handler, state, opts \\ []) do
    validate_options!(opts, @upgrade_options)
    frame_limit = positive(opts, :max_frame_bytes, 65_536, 1_048_576)
    rate = positive(opts, :max_messages_per_second, 50, 1_000)
    mailbox = positive(opts, :max_mailbox_messages, 1_000, 10_000)
    timeout = positive(opts, :idle_timeout, 60_000, 300_000)

    socket_state = %{
      handler: handler,
      state: state,
      frame_limit: frame_limit,
      rate: rate,
      mailbox: mailbox,
      second: nil,
      count: 0
    }

    WebSockAdapter.UpgradeValidation.validate_upgrade!(conn)

    Plug.Conn.upgrade_adapter(
      conn,
      :websocket,
      {Platform.Cowboy.Socket, socket_state,
       %{compress: false, idle_timeout: timeout, max_frame_size: frame_limit}}
    )
  end

  defp tls(:http, nil, nil), do: []

  defp tls(:https, cert, key) when non_empty(cert) and non_empty(key) do
    [
      certfile: cert,
      keyfile: key,
      versions: [:"tlsv1.2", :"tlsv1.3"],
      alpn_preferred_protocols: ["http/1.1"]
    ]
  end

  defp tls(_scheme, _cert, _key),
    do: raise(ArgumentError, "HTTPS requires both certfile and keyfile; HTTP accepts neither")

  defp valid_plug?(plug) when is_atom(plug), do: true
  defp valid_plug?({module, _opts}) when is_atom(module), do: true
  defp valid_plug?(_plug), do: false

  defp check!(true, _message), do: :ok
  defp check!(false, message), do: raise(ArgumentError, message)

  defp valid_ip?(ip) when is_tuple(ip) and tuple_size(ip) == 4 do
    ip |> Tuple.to_list() |> Enum.all?(&(is_integer(&1) and &1 in 0..255))
  end

  defp valid_ip?(ip) when is_tuple(ip) and tuple_size(ip) == 8 do
    ip |> Tuple.to_list() |> Enum.all?(&(is_integer(&1) and &1 in 0..65_535))
  end

  defp valid_ip?(_), do: false

  defp validate_options!(opts, allowed) do
    check!(
      Keyword.keyword?(opts) and unique_allowed?(Keyword.keys(opts), allowed),
      "unknown or duplicate options"
    )
  end

  defp unique_allowed?(keys, allowed),
    do: Enum.all?(keys, &(&1 in allowed)) and length(keys) == length(Enum.uniq(keys))

  defp positive(opts, key, default, max) do
    case Keyword.get(opts, key, default) do
      value when int_in(value, 1, max) -> value
      _ -> raise ArgumentError, "#{key} must be a positive integer within the allowed cap"
    end
  end
end
