# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.OsMonConfigTest do
  use ExUnit.Case, async: false

  test "memsup never raises the host-wide system memory alarm" do
    {:ok, _} = Application.ensure_all_started(:os_mon)

    assert :memsup.get_sysmem_high_watermark() == 100
    refute List.keymember?(:alarm_handler.get_alarms(), :system_memory_high_watermark, 0)
  end
end
