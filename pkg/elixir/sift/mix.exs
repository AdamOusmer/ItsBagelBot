# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule PlatformSift.MixProject do
  use Mix.Project

  def project,
    do: [app: :platform_sift, version: "0.1.0", elixir: "~> 1.17", deps: deps()]

  def application, do: []

  # Commit "Add checksums for v0.6.1": Torque publishes NIF checksums after the tag.
  defp deps,
    do: [
      {:torque,
       git: "https://github.com/pluralsh/torque.git",
       ref: "56c3e69f3847e00bbe0b073ab8c99ee409b01c95"}
    ]
end
