# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.HotPathTest do
  use ExUnit.Case, async: false

  import Ingress.EnvCase

  alias Ingress.{Config, JSON}

  test "native JSON preserves Elixir nil and atom-key wire semantics" do
    encoded = JSON.encode(%{lane: :standard, optional: nil}) |> IO.iodata_to_binary()

    assert encoded =~ ~s("lane":"standard")
    assert encoded =~ ~s("optional":null)
    assert JSON.decode(encoded) == {:ok, %{"lane" => "standard", "optional" => nil}}
    assert {:error, _reason} = JSON.decode("not json")
  end

  test "hot configuration is one immutable persistent-term snapshot" do
    put_env(
      special_user_ids: MapSet.new(["before"]),
      max_chat_text_bytes: 321,
      lane_subject_standard: "lane.before"
    )

    # Registered after put_env/1, so it unwinds first: the snapshot is gone
    # before the config it was taken from is restored.
    on_exit(&Config.uninstall_hot_path/0)
    Config.install_hot_path()

    put_env(
      special_user_ids: MapSet.new(["after"]),
      max_chat_text_bytes: 999,
      lane_subject_standard: "lane.after"
    )

    assert Config.hot_special_user_ids() == MapSet.new(["before"])
    assert Config.hot_max_chat_text_bytes() == 321
    assert Config.hot_lane_subject(:standard) == "lane.before"
  end
end
