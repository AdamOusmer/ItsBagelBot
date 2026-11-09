defmodule Platform.StartupTest do
  use ExUnit.Case, async: true

  test "disabled services run base only and no hooks" do
    owner = self()

    {:ok, pid} =
      Platform.Startup.start_link(
        name: :startup_disabled_test,
        enabled: false,
        base: fn -> [] end,
        service: fn ->
          send(owner, :service)
          []
        end,
        install: fn -> send(owner, :install) end
      )

    refute_receive _
    Supervisor.stop(pid)
  end

  test "failed service construction uninstalls initialized state" do
    owner = self()

    assert_raise RuntimeError, fn ->
      Platform.Startup.start_link(
        name: :startup_failure_test,
        enabled: true,
        base: fn -> [] end,
        service: fn -> raise "invalid service" end,
        install: fn -> send(owner, :installed) end,
        uninstall: fn -> send(owner, :uninstalled) end
      )
    end

    assert_receive :installed
    assert_receive :uninstalled
  end

  test "partial install failure runs cleanup once" do
    owner = self()

    assert_raise RuntimeError, "install failed", fn ->
      Platform.Startup.start_link(
        name: :startup_install_failure_test,
        enabled: true,
        base: fn -> [] end,
        service: fn -> [] end,
        install: fn ->
          send(owner, :installed)
          raise "install failed"
        end,
        uninstall: fn -> send(owner, :uninstalled) end
      )
    end

    assert_receive :installed
    assert_receive :uninstalled
    refute_receive :uninstalled
  end

  test "a failing cleanup is never invoked a second time" do
    owner = self()

    assert_raise RuntimeError, "cleanup failed", fn ->
      Platform.Startup.start_link(
        name: :startup_cleanup_failure_test,
        enabled: true,
        base: fn -> [] end,
        service: fn -> raise "construction failed" end,
        uninstall: fn ->
          send(owner, :uninstalled)
          raise "cleanup failed"
        end
      )
    end

    assert_receive :uninstalled
    refute_receive :uninstalled
  end

  test "supervisor startup error runs cleanup once" do
    owner = self()

    {:ok, existing} =
      Supervisor.start_link([], strategy: :one_for_one, name: :startup_duplicate_test)

    assert {:error, {:already_started, ^existing}} =
             Platform.Startup.start_link(
               name: :startup_duplicate_test,
               enabled: true,
               base: fn -> [] end,
               service: fn -> [] end,
               uninstall: fn -> send(owner, :uninstalled) end
             )

    assert_receive :uninstalled
    refute_receive :uninstalled
    Supervisor.stop(existing)
  end
end
