# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Backoff do
  @moduledoc "Jittered delay math for retries, reconnects and drains."

  def jittered_ms(attempts, base_ms, max_ms, cap_exponent) do
    min(base_ms * Integer.pow(2, min(attempts - 1, cap_exponent)), max_ms) + :rand.uniform(1_000)
  end

  def uniform_ms(max_ms) when is_integer(max_ms) and max_ms >= 1, do: :rand.uniform(max_ms)

  def between_ms(min_ms, max_ms) when is_integer(min_ms) and max_ms >= min_ms do
    min_ms + :rand.uniform(max_ms - min_ms + 1) - 1
  end
end
