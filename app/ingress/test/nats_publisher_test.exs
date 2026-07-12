defmodule Ingress.Nats.PublisherTest do
  # async: false — the publisher uses a named process, a named ETS table and a
  # global persistent_term context, so it cannot share the VM with a parallel
  # instance of itself.
  use ExUnit.Case, async: false

  alias Ingress.Nats.Publisher

  defmodule FakeGnat do
    use GenServer

    def start_link(opts),
      do: GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))

    def init(opts), do: {:ok, %{test: Keyword.fetch!(opts, :test), sid: 0}}

    def handle_call({:sub, _receiver, _topic, _opts}, _from, state) do
      {:reply, {:ok, state.sid + 1}, %{state | sid: state.sid + 1}}
    end

    def handle_call({:pub, topic, message, opts}, _from, state) do
      send(state.test, {:pub, topic, message, opts})
      {:reply, :ok, state}
    end
  end

  describe "id_from_topic/2" do
    @prefix "_INBOX.ingresspub.abc123."

    test "extracts the reply id under this collector's prefix" do
      assert Publisher.id_from_topic(@prefix <> "42", @prefix) == 42
    end

    test "ignores a subject outside the prefix" do
      assert Publisher.id_from_topic("twitch.ingress.event.premium", @prefix) == nil
    end

    test "ignores a non-integer or trailing-garbage suffix" do
      assert Publisher.id_from_topic(@prefix <> "nope", @prefix) == nil
      assert Publisher.id_from_topic(@prefix <> "4x", @prefix) == nil
      assert Publisher.id_from_topic(@prefix, @prefix) == nil
    end
  end

  describe "enqueue/3 admission" do
    setup do
      prev = Application.get_env(:ingress, :publish_max_pending)
      Application.put_env(:ingress, :publish_max_pending, 2)

      # Stand in for PublisherPool: one shard, its BUS connection deliberately
      # absent so the underlying pub never leaves the VM.
      start_supervised!({Publisher, [index: 0, conn: :gnat_bus_pub_test]})
      :persistent_term.put({Publisher, :n}, 1)

      on_exit(fn ->
        :persistent_term.erase({Publisher, :n})

        if prev do
          Application.put_env(:ingress, :publish_max_pending, prev)
        else
          Application.delete_env(:ingress, :publish_max_pending)
        end
      end)

      %{ctx: :persistent_term.get({Publisher, :ctx, 0})}
    end

    test "refuses once the in-flight window is full and does not leak a slot", %{ctx: ctx} do
      # Index 1 is the outstanding-publish count. Saturate it directly.
      :atomics.put(ctx.counter, 1, 2)

      assert Publisher.enqueue("twitch.ingress.event.standard", "{}", nil) ==
               {:error, :overloaded}

      # A refused admission must roll its increment back.
      assert :atomics.get(ctx.counter, 1) == 2
    end

    test "uses a constant-time hash set for pending publishes", %{ctx: ctx} do
      assert :ets.info(ctx.table, :type) == :set
    end

    test "drops to :not_connected when the shard's BUS connection is absent", %{ctx: ctx} do
      # The shard's connection is not registered, so the underlying pub exits and
      # the publish is undone rather than left outstanding.
      assert Publisher.enqueue("twitch.ingress.event.standard", "{}", nil) ==
               {:error, :not_connected}

      assert :atomics.get(ctx.counter, 1) == 0
    end

    test "flushes aggregate outcome counters instead of emitting per-event metrics", %{ctx: ctx} do
      :atomics.put(ctx.counter, 3, 12_000)
      :atomics.put(ctx.counter, 4, 7)
      :atomics.put(ctx.counter, 5, 2)

      send(Publisher.process_name(0), :gauge)
      _state = :sys.get_state(Publisher.process_name(0))

      assert :atomics.get(ctx.counter, 3) == 0
      assert :atomics.get(ctx.counter, 4) == 0
      assert :atomics.get(ctx.counter, 5) == 0
    end

    test "a saturated local shard falls through to spare publisher capacity", %{ctx: ctx} do
      conn = :gnat_bus_pub_fallback_test
      start_supervised!({FakeGnat, [name: conn, test: self()]})

      start_supervised!(
        Supervisor.child_spec({Publisher, [index: 1, conn: conn]}, id: :fallback_publisher)
      )

      :persistent_term.put({Publisher, :n}, 2)

      :atomics.put(ctx.counter, 1, 2)

      assert Publisher.enqueue("twitch.ingress.event.standard", "{}", "msg-1") == :ok
      assert_receive {:pub, "twitch.ingress.event.standard", "{}", _opts}, 500
      assert :atomics.get(ctx.counter, 1) == 2
    end
  end
end
