defmodule Platform.SiftTest do
  use ExUnit.Case, async: true

  alias Platform.Sift
  alias Platform.Sift.{DecodeError, EncodeError}

  @doc_json ~s({"account_id":"abc-123","level":74,"ratio":1.75,"active":true,"missing_child":null,) <>
              ~s("name":"say \\"hi\\"","profile":{"region":"eu","wins":12},"modes":["solo","duo","squad"],) <>
              ~s("odd/key~":"x"})

  test "decode maps null to nil and reports malformed input as a DecodeError" do
    assert {:ok, %{"a" => nil, "b" => [1, 2.5, true]}} =
             Sift.decode(~s({"a":null,"b":[1,2.5,true]}))

    for json <- ["{", ~s({"a":1} x), <<?", 0xFF, ?">>, ""] do
      assert {:error, %DecodeError{} = error} = Sift.decode(json), inspect(json)
      assert Exception.message(error) =~ "invalid JSON"
    end

    assert {:error, %DecodeError{reason: :not_a_binary}} = Sift.decode(nil)
    assert_raise DecodeError, fn -> Sift.decode!("[") end
    assert Sift.valid?(~s({"ok":true}))
    refute Sift.valid?("[")
  end

  test "encode writes nil as null, atoms as strings and date-times as ISO 8601" do
    assert Sift.encode!(%{a: [nil, :x, ~U[2026-07-12 18:23:44Z], ~D[2026-07-12]]}) ==
             ~s({"a":[null,"x","2026-07-12T18:23:44Z","2026-07-12"]})

    assert Sift.encode!(%{1 => "one"}) == ~s({"1":"one"})
    assert Sift.encode!(%{"a" => 1}, pretty: true) == ~s({\n  "a": 1\n})
    assert IO.iodata_to_binary(Sift.encode_to_iodata!([1])) == "[1]"

    assert Sift.decode!(Sift.encode!(%{"q" => ~s(quote " and \\ slash\n)})) == %{
             "q" => ~s(quote " and \\ slash\n)
           }
  end

  test "unencodable terms are an EncodeError, raised by encode!" do
    for term <- [{1, 2}, %URI{}, <<0xFF>>] do
      assert {:error, %EncodeError{}} = Sift.encode(term), inspect(term)
    end

    assert_raise EncodeError, fn -> Sift.encode!({1, 2}) end
  end

  test "scan reads prepared paths in order, with nil for missing or null" do
    pointers =
      Sift.pointers([
        ["level"],
        ["profile", "region"],
        ["modes", 1],
        ["missing_child"],
        ["absent"],
        ["odd/key~"],
        []
      ])

    assert {:ok, [74, "eu", "duo", nil, nil, "x", root]} = Sift.scan(@doc_json, pointers)
    assert root == Sift.decode!(@doc_json)
    assert {:error, %DecodeError{}} = Sift.scan("{", pointers)
  end

  test "extract functions mirror Go's Extract* helpers" do
    assert {:ok, "abc-123", :string} = Sift.extract_value(@doc_json, ["account_id"])

    assert {:ok, %{"region" => "eu", "wins" => 12}, :object} =
             Sift.extract_value(@doc_json, ["profile"])

    assert {:ok, nil, :null} = Sift.extract_value(@doc_json, ["missing_child"])
    assert {:error, :not_found} = Sift.extract_value(@doc_json, ["nope"])
    assert {:error, :not_found} = Sift.extract_value(@doc_json, ["modes", 7])
    assert {:error, %DecodeError{}} = Sift.extract_value("{", ["a"])

    assert {:ok, ~s(say "hi")} = Sift.extract_string(@doc_json, ["name"])
    assert {:ok, 12} = Sift.extract_int(@doc_json, ["profile", "wins"])
    assert {:error, {:wrong_kind, :number}} = Sift.extract_int(@doc_json, ["ratio"])
    assert {:ok, 74.0} = Sift.extract_float(@doc_json, ["level"])
    assert {:ok, true} = Sift.extract_bool(@doc_json, ["active"])
    assert {:error, {:wrong_kind, :string}} = Sift.extract_bool(@doc_json, ["name"])
    assert {:ok, "x"} = Sift.extract_string(@doc_json, ["odd/key~"])
  end

  test "extract_each and extract_array reduce with early halt" do
    assert {:ok, 12} =
             Sift.extract_each(@doc_json, ["profile"], 0, fn
               "wins", wins, _acc -> {:halt, wins}
               _key, _value, acc -> {:cont, acc}
             end)

    assert {:ok, ["squad", "duo", "solo"]} =
             Sift.extract_array(@doc_json, ["modes"], [], &{:cont, [&1 | &2]})

    assert {:ok, ["solo"]} = Sift.extract_array(@doc_json, ["modes"], [], &{:halt, [&1 | &2]})

    assert {:error, {:wrong_kind, :array}} =
             Sift.extract_each(@doc_json, ["modes"], [], fn _key, _value, acc -> {:cont, acc} end)

    assert {:error, :not_found} = Sift.extract_array(@doc_json, ["x"], [], &{:cont, [&1 | &2]})
  end
end
