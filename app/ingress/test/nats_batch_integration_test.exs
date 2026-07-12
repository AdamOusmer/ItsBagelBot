defmodule Ingress.NatsBatchIntegrationTest do
  use ExUnit.Case, async: false

  alias Ingress.Nats.Publisher

  @moduletag :integration

  test "Gnat-managed connection writes one atomic batch directly to NATS 2.14" do
    port = System.get_env("NATS_INTEGRATION_PORT")

    if is_nil(port) do
      :ok
    else
      port = String.to_integer(port)
      conn = :gnat_batch_integration
      previous_size = Application.get_env(:ingress, :publish_batch_size)
      previous_wait = Application.get_env(:ingress, :publish_batch_wait_ms)
      Application.put_env(:ingress, :publish_batch_size, 2)
      Application.put_env(:ingress, :publish_batch_wait_ms, 100)

      {:ok, gnat} = Gnat.start_link(%{host: ~c"127.0.0.1", port: port}, name: conn)
      start_supervised!({Publisher, [index: 0, conn: conn]})
      :persistent_term.put({Publisher, :n}, 1)

      on_exit(fn ->
        if Process.alive?(gnat), do: GenServer.stop(gnat)
        :persistent_term.erase({Publisher, :n})
        restore_env(:publish_batch_size, previous_size)
        restore_env(:publish_batch_wait_ms, previous_wait)
      end)

      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":1}), "elixir-1") == :ok
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":2}), "elixir-2") == :ok

      ctx = :persistent_term.get({Publisher, :ctx, 0})
      assert eventually(fn -> :atomics.get(ctx.counter, 1) == 0 end)

      assert {:ok, %{body: body}} =
               Gnat.request(conn, "$JS.API.STREAM.INFO.ELIXIR_BATCH_TEST", "")

      assert {:ok, %{"state" => %{"messages" => 2}}} = Ingress.JSON.decode(body)
    end
  end

  defp eventually(check, attempts \\ 100)

  defp eventually(check, attempts) do
    cond do
      check.() ->
        true

      attempts == 0 ->
        false

      true ->
        Process.sleep(10)
        eventually(check, attempts - 1)
    end
  end

  defp restore_env(key, nil), do: Application.delete_env(:ingress, key)
  defp restore_env(key, value), do: Application.put_env(:ingress, key, value)
end
