# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Backoff do
  def jittered_ms(attempts, base_ms, max_ms, cap_exponent) do
    min(base_ms * Integer.pow(2, min(attempts - 1, cap_exponent)), max_ms) + :rand.uniform(1_000)
  end
end
