# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.ChatDistributionTest do
  use ExUnit.Case, async: true

  alias YtIngress.ChatDistribution

  defp member(node, status \\ :alive),
    do: %{name: {YtIngress.ChatSupervisor, node}, status: status}

  defp session_spec(channel_id) do
    %{
      id: {:chat, channel_id},
      start:
        {YtIngress.ChatSession, :start_link,
         [
           [
             registry_key: {:chat, channel_id},
             channel_id: channel_id,
             live_chat_id: "chat-#{channel_id}"
           ]
         ]},
      restart: :transient
    }
  end

  test "places sessions deterministically regardless of member order" do
    a = [member(:yt_ingress@node1), member(:yt_ingress@node2)]
    b = Enum.reverse(a)

    for id <- ["UCa", "UCb", "UCc", "UCd"] do
      assert ChatDistribution.choose_node(session_spec(id), a) ==
               ChatDistribution.choose_node(session_spec(id), b)
    end
  end

  test "hash placement spreads channels across nodes without stacking them" do
    members = [member(:yt_ingress@node2), member(:yt_ingress@node1)]
    channels = for i <- 0..7, do: "UCchannel#{i}"

    placements =
      for id <- channels do
        {:ok, %{name: {_sup, node}}} = ChatDistribution.choose_node(session_spec(id), members)
        node
      end

    # Hash placement is not the Twitch round-robin — it does not guarantee a
    # perfect split — but it must never stack every session on one node.
    frequencies = Enum.frequencies(placements)
    assert MapSet.size(MapSet.new(Map.values(frequencies))) == length(members)
    assert Enum.max(Map.values(frequencies)) - Enum.min(Map.values(frequencies)) <= 3
  end

  test "dead members receive nothing" do
    members = [member(:yt_ingress@node1), member(:yt_ingress@node2, :dead)]

    for id <- ["UCa", "UCb"] do
      assert {:ok, %{name: {_sup, :yt_ingress@node1}}} =
               ChatDistribution.choose_node(session_spec(id), members)
    end
  end

  test "no alive members is an error" do
    assert {:error, :no_alive_nodes} =
             ChatDistribution.choose_node(session_spec("UCa"), [member(:yt_ingress@node1, :dead)])
  end

  test "non-session children fall back to uniform distribution" do
    members = [member(:yt_ingress@node1), member(:yt_ingress@node2)]
    spec = %{id: :watcher, start: {YtIngress.Watcher, :start_link, [[]]}}

    assert {:ok, %{name: {_sup, node}}} = ChatDistribution.choose_node(spec, members)
    assert node in [:yt_ingress@node1, :yt_ingress@node2]
  end
end
