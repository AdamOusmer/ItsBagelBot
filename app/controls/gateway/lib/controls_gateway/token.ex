# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Token do
  @moduledoc """
  Verification-only seam. This service never issues tokens: Users owns grants
  and signing. See context.md for the wire format.
  """

  @type claims :: %{
          sub: String.t(),
          device_id: String.t(),
          grant_id: String.t(),
          jti: String.t(),
          iat: integer(),
          exp: integer()
        }

  @callback verify(token :: String.t()) :: {:ok, claims()} | {:error, atom()}
end
