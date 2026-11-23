# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.SignedCookie do
  def put(conn, name, claims, options) do
    payload = claims |> Jason.encode!() |> Base.url_encode64(padding: false)
    signature = payload |> sign() |> Base.url_encode64(padding: false)
    Plug.Conn.put_resp_cookie(conn, name, payload <> "." <> signature, options)
  end

  def read(source, name) do
    with true <- usable_key?(key()),
         {:ok, cookie} <- Map.fetch(cookies(source), name),
         [payload, signature] <- :binary.split(cookie, "."),
         {:ok, mac} when byte_size(mac) == 32 <- Base.url_decode64(signature, padding: false),
         true <- :crypto.hash_equals(mac, sign(payload)),
         {:ok, json} <- Base.url_decode64(payload, padding: false),
         {:ok, %{"exp" => exp} = claims} <- Jason.decode(json),
         true <- unexpired?(exp) do
      {:ok, claims}
    else
      _ -> :error
    end
  end

  def key, do: Application.get_env(:controls_gateway, :overlay_cookie_key)

  def mac(data), do: :crypto.mac(:hmac, :sha256, key(), data)

  def now, do: System.system_time(:second)

  defp cookies(%Plug.Conn{} = conn) do
    conn |> Plug.Conn.get_req_header("cookie") |> Enum.join("; ") |> Plug.Conn.Cookies.decode()
  end

  defp cookies(req), do: "cookie" |> :cowboy_req.header(req, "") |> Plug.Conn.Cookies.decode()

  defp usable_key?(key) when is_binary(key), do: byte_size(key) >= 32
  defp usable_key?(_key), do: false

  defp unexpired?(exp) when is_integer(exp), do: exp > now()
  defp unexpired?(_exp), do: false

  defp sign(payload), do: mac(payload)
end
