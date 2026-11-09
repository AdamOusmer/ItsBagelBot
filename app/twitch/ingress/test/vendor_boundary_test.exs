# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Ingress.VendorBoundaryTest do
  use ExUnit.Case, async: true

  test "service and test code reach vendor libraries only through shared packages" do
    files = Path.wildcard(Path.expand("../{lib,config,test}/**/*.{ex,exs}", __DIR__))
    assert files != []
    assert Platform.Boundary.violations(files) == []
  end
end
