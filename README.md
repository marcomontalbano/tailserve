<p align="center">
    <picture>
        <source media="(prefers-color-scheme: dark)" srcset="./images/tailserve-light.svg">
        <source media="(prefers-color-scheme: light)" srcset="./images/tailserve-dark.svg">
        <img alt="tailserve" src="./images/tailserve-dark.svg" width="120">
    </picture>
</p>

<p align="center">
    Your dev servers, on your tailnet.
</p>

<p align="center">
    tailserve watches the development servers running on your machine and makes the ones you allow reachable from every device in your <a href="https://tailscale.com">Tailscale</a> network. Automatically, with a dashboard that links to all of them.
</p>

<p align="center">
    <a href="https://github.com/sponsors/marcomontalbano"><img alt="Sponsor" src="https://img.shields.io/badge/-Sponsor-fafbfc?logo=GitHub%20Sponsors&style=for-the-badge" /></a>
</p>


## :thinking: Why?

If you use Tailscale, you've probably used [`tailscale serve`](https://tailscale.com/docs/features/tailscale-serve) to access a local dev server from your phone. I did too.

The routine was always the same: `npm run dev`, then `tailscale serve --bg 5173`, then grab my phone. Then I'd start another project on `5174`, maybe a Storybook on `6006`, and repeat the whole thing.

Eventually, I'd stop everything but forget to clean up the Tailscale mappings. The next day, I'd find a bunch of stale entries pointing to ports where nothing was running anymore.

Don't get me wrong, I love Tailscale Serve. I often work remotely, sometimes from public networks, so being able to access my local dev servers from my other devices without exposing them to the internet is something I really appreciate. I just got tired of managing the mappings myself.

So I wrote a tool that does it for me.

Start a dev server, and it shows up on your tailnet. Stop it, and the mapping disappears.

That's it. That's `tailserve`.


## :rocket: Quickstart

You need macOS and [Tailscale](https://tailscale.com/download) logged in to a tailnet.

```sh
# install
go install github.com/marcomontalbano/tailserve@latest

# start a dev server as usual, then pick what to serve
tailserve allow

# serve it
tailserve start
```

`tailserve start` prints the address of the dashboard, something like `https://my-laptop.your-tailnet.ts.net/`. Open it from any device on your tailnet :sunglasses:

> [!NOTE]
> Don't have Go? Install it with `brew install go`, or see [go.dev/dl](https://go.dev/dl/).

> [!TIP]
> `go install` puts `tailserve` in `~/go/bin`.\
> If your shell can't find it, add this line to your shell's startup file (e.g. `~/.zshrc`) and open a new terminal:
>
> ```sh
> export PATH="$PATH:$HOME/go/bin"
> ```


## :sparkles: In Short

- **Nothing is served until you allow it.** You decide with a few Rules: a port, a range of ports, a process name like `node`, or a port and a process together.
- **It follows your dev servers.** Every 2 seconds tailserve looks at what's listening. When an allowed server starts, it's served; when it stops (or crashes), it's gone.
- **Sensitive ports are locked.** SSH, databases, debuggers like the Node inspector and screen sharing are never served by a broad Rule, only by one that names that exact port.
- **Nothing is left behind.** Everything tailserve serves lives in its own Tailscale session, so stopping tailserve (or a crash) takes it all offline.
- **It never touches what you served yourself.** Mappings you created with `tailscale serve` are shown, never changed.
- **A dashboard** at your machine's tailnet address, and at http://localhost:7171 on the machine itself, with a link to every server, grouped by repository.


## :book: Usage

```sh
tailserve start     # serve the allowed Services until stopped
tailserve stop      # stop the running tailserve; everything it served goes offline
tailserve status    # what's allowed or served, and why
tailserve allow     # pick running Services to serve, or add a Rule directly
tailserve deny      # pick Rules to remove
tailserve config    # open the config file in $EDITOR
```

### Allow and deny

Run `tailserve allow` without flags and it asks you which of the running servers to serve, and how the Rule should match each one:

- port `5173`, only when `node` listens on it
- port `5173`, whatever process listens on it
- any `node` process, on any port

Or add a Rule directly:

```sh
tailserve allow --port 5173-5199 --note vite   # a range of ports
tailserve allow --process node                 # every node process
tailserve allow --port 8000 --process python3  # both must match

tailserve deny --process node
```

A running tailserve picks up the change within 2 seconds.

### Status

`tailserve status` tells you, for each server, whether it's served and why:

```
PORT    TOOL                   REPOSITORY                  PID     STATE         DETAIL
4321    astro dev (node)       marcomontalbano.github.io   52647   served        https://my-laptop.your-tailnet.ts.net:4321
5180    storybook dev (node)   tailserve                   46138   not allowed   no Rule matches
9229    node                   tailserve                   46139   locked        port 9229 is locked: only a Rule naming exactly this port serves it
```

Add `--all` to also list the servers no Rule matches.


## :gear: Config

The config lives in `~/.config/tailserve/config.toml`. `tailserve allow` and `tailserve deny` write it for you; `tailserve config` opens it if you prefer editing by hand.

```toml
[[allow]]
port = "5173-5199"   # a port or a range of ports
process = "node"     # a process name; with a port too, both must match
note = "vite"        # a reminder of what the Rule is for

[locked]
ports = [4000]       # more locked ports: only a Rule naming one exactly serves it

[dashboard]
local_port = 7171    # the dashboard at http://localhost:7171; 0 turns it off
```

### Locked ports

These ports are never served by a range or a process name, only by a Rule that names that exact port:

`22` SSH · `139` `445` SMB · `548` AFP · `2345` Delve · `3306` MySQL · `3389` Remote Desktop · `5432` PostgreSQL · `5900` VNC · `6379` Redis · `9229` Node inspector · `27017` MongoDB

> [!NOTE]
> Ports `443` and `80` on the tailnet are taken by the dashboard and its redirect.


## :hammer_and_wrench: Development stuff

### Requirements

* macOS
* [Go](https://go.dev/dl/) 1.27.4 or greater (`brew install go`)
* [Tailscale](https://tailscale.com/download) logged in to a tailnet

### Setup

```sh
git clone https://github.com/marcomontalbano/tailserve.git
cd tailserve

go test ./...        # run the tests

go run . status      # run the CLI from the sources

go build -o bin/tailserve .                                 # development build
go build -trimpath -ldflags="-s -w" -o bin/tailserve .      # smaller build, for releases

gofmt -l .           # list the files that aren't formatted (no output means all good)
go fmt ./...         # format them
go vet ./...         # catch suspicious code the compiler accepts

# check the dependencies for known vulnerabilities
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

To try a different config without touching yours, point `XDG_CONFIG_HOME` at another folder:

```sh
XDG_CONFIG_HOME=/tmp/tailserve-dev go run . status   # reads /tmp/tailserve-dev/tailserve/config.toml
```
