# Tailserve

Serves the development servers running on your machine to the rest of your
tailnet, automatically, behind a single dashboard. Nothing is served until you
allow it.

## Development

Requires macOS, [Go](https://go.dev/dl/) 1.27.4 or later (`brew install go`)
and [Tailscale](https://tailscale.com/download) logged in to a tailnet.

```sh
go test ./...        # run the tests
```
