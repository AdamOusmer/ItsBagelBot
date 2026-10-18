# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialValkeyFixture do
  alias Ingress.TrialValkey

  def start do
    dir = Path.join(System.tmp_dir!(), "bagel-trial-#{System.unique_integer([:positive])}")
    File.mkdir_p!(dir)
    port = free_port()
    previous = System.get_env("VALKEY_ADDR")
    System.put_env("VALKEY_ADDR", "127.0.0.1:#{port}")

    {_output, 0} =
      System.cmd("valkey-server", [
        "--bind",
        "127.0.0.1",
        "--port",
        Integer.to_string(port),
        "--save",
        "",
        "--appendonly",
        "no",
        "--daemonize",
        "yes",
        "--pidfile",
        Path.join(dir, "valkey.pid")
      ])

    {dir, previous}
  end

  def await_ready do
    Enum.reduce_while(1..50, :unavailable, fn _, _ ->
      case TrialValkey.command(["PING"]) do
        {:ok, "PONG"} ->
          {:halt, :ok}

        _ ->
          Process.sleep(20)
          {:cont, :unavailable}
      end
    end)
  end

  def stop({dir, previous}) do
    _ = TrialValkey.command(["SHUTDOWN", "NOSAVE"])

    if previous,
      do: System.put_env("VALKEY_ADDR", previous),
      else: System.delete_env("VALKEY_ADDR")

    File.rm_rf!(dir)
  end

  defp free_port do
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, ip: {127, 0, 0, 1}])
    {:ok, {_address, port}} = :inet.sockname(listener)
    :gen_tcp.close(listener)
    port
  end
end
