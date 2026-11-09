# Shared WebSocket client

`Platform.WebSocket.Client` wraps an asynchronous Mint HTTP/1 WebSocket connection.
It owns no reconnect or application policy. The caller streams process messages,
handles frames, and closes the returned connection. `connect/2` accepts
`:transport_opts` and upgrade request `:headers`. A refused upgrade reports
`{:http_status, status}`; transport and protocol errors report the plain reason
(for example `:closed` or `:econnrefused`), never a Mint struct.
