# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.TestSupport.Jetstream do
  @moduledoc "JetStream API helpers for integration tests."

  alias Platform.Nats
  alias Platform.Nats.Planes

  def api!(subject, body) do
    {:ok, %{body: reply}} =
      Planes.jetstream_request({subject, Jason.encode!(body), [receive_timeout: 2_000]})

    Jason.decode!(reply)
  end

  def create_stream!(name, subjects) do
    %{"config" => _} = api!("$JS.API.STREAM.CREATE." <> name, %{name: name, subjects: subjects})
    :ok
  end

  def create_consumer!(stream, durable, config \\ %{}) do
    config = Map.merge(%{durable_name: durable, ack_policy: "explicit"}, config)
    body = %{stream_name: stream, config: config}
    %{"config" => _} = api!("$JS.API.CONSUMER.DURABLE.CREATE.#{stream}.#{durable}", body)
    :ok
  end

  def consumer_info!(stream, durable), do: api!("$JS.API.CONSUMER.INFO.#{stream}.#{durable}", %{})

  def publish!(subject, body) do
    {:ok, %{body: reply}} = Planes.jetstream_request({subject, body, [receive_timeout: 2_000]})
    %{"seq" => _} = Jason.decode!(reply)
    :ok
  end

  def next(stream, durable, expires_ms) do
    body = Jason.encode!(%{batch: 1, expires: expires_ms * 1_000_000})
    inbox = "_INBOX.test." <> Base.url_encode64(:crypto.strong_rand_bytes(9), padding: false)
    {:ok, sid} = Planes.subscribe(:jetstream, self(), inbox)

    :ok =
      Nats.publish(:gnat_jetstream, "$JS.API.CONSUMER.MSG.NEXT.#{stream}.#{durable}", body,
        reply_to: inbox
      )

    receive do
      {:msg, %{sid: ^sid} = message} -> {:ok, message}
    after
      expires_ms + 1_000 -> {:error, :timeout}
    end
  end

  def eventually(fun, attempts \\ 60)
  def eventually(fun, 1), do: fun.()

  def eventually(fun, attempts) do
    fun.() || (Process.sleep(100) && eventually(fun, attempts - 1))
  end
end
