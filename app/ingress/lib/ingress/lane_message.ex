defmodule Ingress.LaneMessage do
  @moduledoc """
  A lane publish whose members are already encoded except for `lane`.

  `stream.online`/`stream.offline` are dual-published: the same event goes to
  the live lane *and* to the broadcaster's own premium/standard lane, and the
  two documents differ in exactly one field. Encoding the shared members once
  and prefixing each copy with its own lane leaves a single encode for both
  publishes, and both copies then reference the same binaries in the
  publisher's in-flight window instead of holding two independent copies.

  `body` is the output of `Ingress.JSON.members/1` — the object's members
  without the enclosing braces. `Ingress.JSON` closes the object around it.
  """

  @enforce_keys [:lane, :body]
  defstruct [:lane, :body]

  @type t :: %__MODULE__{lane: atom(), body: iodata()}
end
