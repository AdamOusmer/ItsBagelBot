# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Sift do
  @moduledoc """
  JSON for the Elixir services: a thin wrapper over Torque (sonic-rs), shaped
  like Go's `pkg/codec` over sonic. `null` decodes to `nil`. Paths are lists of
  object keys and array indexes, like Go's `codec.Path`.
  """

  alias Platform.Sift.{DecodeError, EncodeError, Pack}

  @type path :: [String.t() | non_neg_integer()]
  @type kind :: :string | :number | :object | :array | :bool | :null

  @spec decode(binary()) :: {:ok, term()} | {:error, DecodeError.t()}
  def decode(json) when is_binary(json), do: json |> Torque.decode() |> decoded()
  def decode(_json), do: {:error, %DecodeError{reason: :not_a_binary}}

  @spec decode!(binary()) :: term()
  def decode!(json) do
    case decode(json) do
      {:ok, value} -> value
      {:error, error} -> raise error
    end
  end

  @spec valid?(binary()) :: boolean()
  def valid?(json), do: match?({:ok, _}, decode(json))

  @doc "Encodes to a binary. `pretty: true` indents with two spaces."
  @spec encode(term(), pretty: boolean()) :: {:ok, binary()} | {:error, EncodeError.t()}
  def encode(term, opts \\ []) do
    case Torque.encode(term, opts) do
      {:ok, json} -> {:ok, json}
      {:error, reason} -> {:error, %EncodeError{reason: reason}}
    end
  end

  @spec encode!(term(), pretty: boolean()) :: binary()
  def encode!(term, opts \\ []) do
    case encode(term, opts) do
      {:ok, json} -> json
      {:error, error} -> raise error
    end
  end

  @spec encode_to_iodata!(term()) :: iodata()
  def encode_to_iodata!(term), do: encode!(term)

  @doc """
  Prepares a fixed set of paths once for `scan/2`. Keep the result in
  `:persistent_term` or process state; it is a NIF resource and cannot be a
  module attribute. Assumes documents have no duplicate object keys.
  """
  @spec pointers([path()]) :: term()
  def pointers(paths) when is_list(paths),
    do: paths |> Enum.map(&pointer/1) |> Torque.compile_pointers(unique_keys: true)

  @doc """
  Reads the values at the prepared paths in one pass, building only those
  values. Returns them in path order; a missing path or JSON `null` is `nil`.
  """
  @spec scan(binary(), term()) :: {:ok, [term()]} | {:error, DecodeError.t()}
  def scan(json, pointers) when is_binary(json),
    do: json |> Torque.parse_get_many_nil(pointers) |> decoded()

  @spec extract_value(binary(), path()) ::
          {:ok, term(), kind()} | {:error, :not_found | DecodeError.t()}
  def extract_value(json, path) when is_binary(json) and is_list(path) do
    with {:ok, document} <- json |> Torque.parse() |> decoded() do
      case Torque.get(document, pointer(path)) do
        {:ok, value} -> {:ok, value, kind(value)}
        {:error, :no_such_field} -> {:error, :not_found}
        {:error, reason} -> {:error, %DecodeError{reason: reason}}
      end
    end
  end

  @spec extract_string(binary(), path()) :: {:ok, String.t()} | {:error, term()}
  def extract_string(json, path), do: typed(json, path, :string)

  @spec extract_int(binary(), path()) :: {:ok, integer()} | {:error, term()}
  def extract_int(json, path) do
    case extract_value(json, path) do
      {:ok, value, :number} when is_integer(value) -> {:ok, value}
      {:ok, _value, kind} -> {:error, {:wrong_kind, kind}}
      error -> error
    end
  end

  @spec extract_float(binary(), path()) :: {:ok, float()} | {:error, term()}
  def extract_float(json, path) do
    with {:ok, value} <- typed(json, path, :number), do: {:ok, value / 1}
  end

  @spec extract_bool(binary(), path()) :: {:ok, boolean()} | {:error, term()}
  def extract_bool(json, path), do: typed(json, path, :bool)

  @doc "Reduces over the object at `path`; `fun` gets key, value and acc, and returns `{:cont | :halt, acc}`."
  @spec extract_each(binary(), path(), acc, (String.t(), term(), acc -> {:cont | :halt, acc})) ::
          {:ok, acc} | {:error, term()}
        when acc: term()
  def extract_each(json, path, acc, fun) when is_function(fun, 3) do
    with {:ok, object} <- typed(json, path, :object) do
      {:ok, reduce(Map.to_list(object), acc, fn {key, value}, acc -> fun.(key, value, acc) end)}
    end
  end

  @doc "Reduces over the array at `path`; `fun` gets value and acc."
  @spec extract_array(binary(), path(), acc, (term(), acc -> {:cont | :halt, acc})) ::
          {:ok, acc} | {:error, term()}
        when acc: term()
  def extract_array(json, path, acc, fun) when is_function(fun, 2) do
    with {:ok, list} <- typed(json, path, :array), do: {:ok, reduce(list, acc, fun)}
  end

  @doc "Compresses with zstd when it saves at least 10%; otherwise returns `src` unchanged."
  @spec pack(binary()) :: binary()
  defdelegate pack(src), to: Pack

  @doc "Reverses `pack/1`; input without the zstd frame magic is returned as is. Output is capped at 4 MiB."
  @spec unpack(binary()) :: {:ok, binary()} | {:error, term()}
  defdelegate unpack(src), to: Pack

  defp decoded({:ok, value}), do: {:ok, value}
  defp decoded({:error, reason}), do: {:error, %DecodeError{reason: reason}}

  defp typed(json, path, expected) do
    case extract_value(json, path) do
      {:ok, value, ^expected} -> {:ok, value}
      {:ok, _value, kind} -> {:error, {:wrong_kind, kind}}
      error -> error
    end
  end

  defp reduce([], acc, _fun), do: acc

  defp reduce([head | tail], acc, fun) do
    case fun.(head, acc) do
      {:cont, acc} -> reduce(tail, acc, fun)
      {:halt, acc} -> acc
    end
  end

  # Torque reads "/" as the root, so an empty path is the root too.
  defp pointer([]), do: "/"
  defp pointer(path), do: Enum.map_join(path, &["/", segment(&1)])

  defp segment(index) when is_integer(index) and index >= 0, do: Integer.to_string(index)

  defp segment(key) when is_binary(key),
    do: key |> String.replace("~", "~0") |> String.replace("/", "~1")

  defp kind(value) when is_binary(value), do: :string
  defp kind(value) when is_number(value), do: :number
  defp kind(value) when is_map(value), do: :object
  defp kind(value) when is_list(value), do: :array
  defp kind(value) when is_boolean(value), do: :bool
  defp kind(nil), do: :null
end
