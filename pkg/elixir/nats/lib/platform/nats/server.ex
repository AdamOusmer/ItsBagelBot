# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Nats.Server do
  @moduledoc "Consumer callback adapter for service-owned request handlers. Every request continues a sampled caller trace."
  defmacro __using__(_) do
    quote do
      use Gnat.Server
      @before_compile Platform.Nats.Server
    end
  end

  defmacro __before_compile__(env) do
    name = inspect(env.module)

    quote do
      defoverridable request: 1

      def request(message) do
        Platform.NewRelic.continue("Rpc", unquote(name), Map.get(message, :headers), fn ->
          super(message)
        end)
      end
    end
  end
end
