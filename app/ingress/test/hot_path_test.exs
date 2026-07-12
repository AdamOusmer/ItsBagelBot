defmodule Ingress.HotPathTest do
  use ExUnit.Case, async: false

  alias Ingress.{Config, JSON}

  test "native JSON preserves Elixir nil and atom-key wire semantics" do
    encoded = JSON.encode(%{lane: :standard, optional: nil}) |> IO.iodata_to_binary()

    assert encoded =~ ~s("lane":"standard")
    assert encoded =~ ~s("optional":null)
    assert JSON.decode(encoded) == {:ok, %{"lane" => "standard", "optional" => nil}}
    assert {:error, _reason} = JSON.decode("not json")
  end

  test "hot configuration is one immutable persistent-term snapshot" do
    previous_special = Application.get_env(:ingress, :special_user_ids)
    previous_max = Application.get_env(:ingress, :max_chat_text_bytes)
    previous_standard = Application.get_env(:ingress, :lane_subject_standard)

    on_exit(fn ->
      Config.uninstall_hot_path()
      restore_env(:special_user_ids, previous_special)
      restore_env(:max_chat_text_bytes, previous_max)
      restore_env(:lane_subject_standard, previous_standard)
    end)

    Application.put_env(:ingress, :special_user_ids, MapSet.new(["before"]))
    Application.put_env(:ingress, :max_chat_text_bytes, 321)
    Application.put_env(:ingress, :lane_subject_standard, "lane.before")
    Config.install_hot_path()

    Application.put_env(:ingress, :special_user_ids, MapSet.new(["after"]))
    Application.put_env(:ingress, :max_chat_text_bytes, 999)
    Application.put_env(:ingress, :lane_subject_standard, "lane.after")

    assert Config.hot_special_user_ids() == MapSet.new(["before"])
    assert Config.hot_max_chat_text_bytes() == 321
    assert Config.hot_lane_subject(:standard) == "lane.before"
  end

  defp restore_env(key, nil), do: Application.delete_env(:ingress, key)
  defp restore_env(key, value), do: Application.put_env(:ingress, key, value)
end
