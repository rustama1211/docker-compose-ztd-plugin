# `docker ztd`

Zero-downtime rollout plugin for Docker Compose.

## Overview

`docker ztd` updates a running Compose service without dropping traffic:

- scale service to 2x replicas
- wait for new containers to become ready
- switch Traefik dynamic config to new container IDs
- remove old containers after a drain wait

You can use either implementation:

- Bash implementation (legacy, simple setup for some environments)
- Go implementation (new modular implementation)

## Installation

### Bash implementation

```bash
curl -fsSL https://gist.githubusercontent.com/rustama1211/17c22c7a6f240d6b6702e413d4dbf5af/raw/32e71ebd81b59b383e5a2c355027b641ca9e1496/install-docker-ztd.sh | bash
```

#### Bash dependencies

You should have `jq` and `yq` installed on your server.

When using the bash-based installation, you may need to create the Traefik folder manually depending on your Docker setup:

```bash
mkdir -p traefik
chown -R $(id -u):$(id -g) traefik
chmod -R 755 traefik
```

### Go implementation

There is no auto-install script for Go yet. Build and install manually:

```bash
go build -o docker-ztd-go ./cmd/docker-ztd
mkdir -p ~/.docker/cli-plugins
cp docker-ztd-go ~/.docker/cli-plugins/docker-ztd
chmod +x ~/.docker/cli-plugins/docker-ztd
```

Go implementation advantage: no runtime dependency on `jq` or `yq`.

## Runtime Dependencies

- Docker CLI
- Compose support:
  - `docker compose` (preferred), or
  - `docker-compose` (fallback)

## Usage

```bash
docker ztd [OPTIONS] SERVICE
```

Examples:

```bash
docker ztd -f docker-compose.yml <service-name>
docker ztd -f docker-compose.yml up -d
```

Options:

- `-h, --help`
- `-f, --file FILE`
- `-t, --timeout N`
- `-w, --wait N`
- `--wait-after-healthy N`
- `--env-file FILE`
- `--proxy TYPE`
- `--traefik-conf FILE`

## Traefik Labels Supported

Minimum required Traefik version: **v3.0**

Labels marked *(v3)* require Traefik v3 and are not available in v2.

**Label keys are case-insensitive.** You can write labels in the camelCase form shown in the official Traefik docs (`ruleSyntax`, `serversTransport`, `passHostHeader`, `responseForwarding.flushInterval`, `httpOnly`, `sameSite`, `maxAge`, `followRedirects`, ...) or in lowercase — both match, mirroring the Traefik docker provider.

**Router names are independent of the compose service name.** One compose service can declare multiple routers under any names. When `.service` is omitted on a router, it defaults to the compose service the labels are attached to — useful for patterns like a dedicated fallback router:

```yaml
services:
  my-fallback-app:
    image: whoami
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.catchall.rule=HostRegexp(`{any:.+}`)"
      - "traefik.http.routers.catchall.priority=1"
      - "traefik.http.routers.catchall.entrypoints=web"
```

### General
- `traefik.enable`

### HTTP Routers
- `traefik.http.routers.<name>.rule`
- `traefik.http.routers.<name>.entrypoints`
- `traefik.http.routers.<name>.middlewares`
- `traefik.http.routers.<name>.service`
- `traefik.http.routers.<name>.priority`
- `traefik.http.routers.<name>.ruleSyntax` *(v3)* — `v2` or `v3`
- `traefik.http.routers.<name>.tls`
- `traefik.http.routers.<name>.tls.certresolver`
- `traefik.http.routers.<name>.tls.options`
- `traefik.http.routers.<name>.tls.domains[n].main`
- `traefik.http.routers.<name>.tls.domains[n].sans`

### HTTP Services
- `traefik.http.services.<name>.loadbalancer.server.port`
- `traefik.http.services.<name>.loadbalancer.server.scheme`
- `traefik.http.services.<name>.loadbalancer.server.weight` *(v3)* — per-server weight for `wrr` strategy
- `traefik.http.services.<name>.loadbalancer.strategy` *(v3)* — `wrr` | `p2c` | `hrw` | `leasttime`
- `traefik.http.services.<name>.loadbalancer.serversTransport` *(v3)* — reference to a serversTransport config
- `traefik.http.services.<name>.loadbalancer.passhostheader`
- `traefik.http.services.<name>.loadbalancer.responseforwarding.flushinterval`
- `traefik.http.services.<name>.loadbalancer.healthCheck.path`
- `traefik.http.services.<name>.loadbalancer.healthCheck.interval`
- `traefik.http.services.<name>.loadbalancer.healthCheck.timeout`
- `traefik.http.services.<name>.loadbalancer.healthCheck.scheme`
- `traefik.http.services.<name>.loadbalancer.healthCheck.mode`
- `traefik.http.services.<name>.loadbalancer.healthCheck.hostname`
- `traefik.http.services.<name>.loadbalancer.healthCheck.port`
- `traefik.http.services.<name>.loadbalancer.healthCheck.headers.<header>`
- `traefik.http.services.<name>.loadbalancer.healthCheck.followRedirects`
- `traefik.http.services.<name>.loadbalancer.healthCheck.method`
- `traefik.http.services.<name>.loadbalancer.healthCheck.status`
- `traefik.http.services.<name>.loadbalancer.sticky.cookie`
- `traefik.http.services.<name>.loadbalancer.sticky.cookie.name`
- `traefik.http.services.<name>.loadbalancer.sticky.cookie.secure`
- `traefik.http.services.<name>.loadbalancer.sticky.cookie.httpOnly`
- `traefik.http.services.<name>.loadbalancer.sticky.cookie.sameSite`
- `traefik.http.services.<name>.loadbalancer.sticky.cookie.maxAge`

### HTTP Middlewares (all types, generic)
All `traefik.http.middlewares.<name>.<type>.*` labels are supported generically.
Dot-notation paths and `[n]` array indices are converted to nested config. Examples:
- `traefik.http.middlewares.<name>.redirectscheme.scheme`
- `traefik.http.middlewares.<name>.redirectscheme.permanent`
- `traefik.http.middlewares.<name>.basicauth.users`
- `traefik.http.middlewares.<name>.basicauth.realm`
- `traefik.http.middlewares.<name>.ratelimit.average`
- `traefik.http.middlewares.<name>.ratelimit.burst`
- `traefik.http.middlewares.<name>.headers.customrequestheaders.<header>`
- `traefik.http.middlewares.<name>.headers.customresponseheaders.<header>`
- `traefik.http.middlewares.<name>.forwardauth.address`
- `traefik.http.middlewares.<name>.forwardauth.trustforwardheader`
- `traefik.http.middlewares.<name>.stripprefix.prefixes`
- `traefik.http.middlewares.<name>.compress.minresponsebodybytes`
- … and all other middleware types

### TCP Routers
- `traefik.tcp.routers.<name>.rule`
- `traefik.tcp.routers.<name>.entrypoints`
- `traefik.tcp.routers.<name>.middlewares`
- `traefik.tcp.routers.<name>.service`
- `traefik.tcp.routers.<name>.tls`
- `traefik.tcp.routers.<name>.tls.passthrough`
- `traefik.tcp.routers.<name>.tls.certresolver`
- `traefik.tcp.routers.<name>.tls.options`

### TCP Services
- `traefik.tcp.services.<name>.loadbalancer.server.port`
- `traefik.tcp.services.<name>.loadbalancer.terminationdelay`
- `traefik.tcp.services.<name>.loadbalancer.proxyprotocol.version`

### UDP Routers
- `traefik.udp.routers.<name>.entrypoints`
- `traefik.udp.routers.<name>.service`

### UDP Services
- `traefik.udp.services.<name>.loadbalancer.server.port`

## Notes

- Avoid `container_name` and fixed host `ports` on services that need multi-replica rollout.
- `nginx-proxy` mode remains not implemented.
