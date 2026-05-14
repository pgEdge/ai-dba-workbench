# TLS and Reverse Proxy Requirements

Any network-accessible deployment of the pgEdge AI DBA
Workbench must terminate TLS in front of the server.
This page documents the supported deployment topology,
the operator responsibilities, and the risks of
ignoring the requirement.

## Why TLS Is Mandatory

The web client sends user credentials, session tokens,
and API tokens to the server on every authenticated
request. Plain HTTP transmits these credentials in
cleartext over the network. Anyone on the path between
the browser and the server can read or replay the
captured credentials.

The workbench therefore treats TLS as a hard
requirement for any deployment that is reachable from a
network other than the loopback interface of the host
running the server. The requirement applies equally to
production deployments, staging deployments, and shared
development environments.

## Recommended Topology

The recommended topology places a TLS-terminating
reverse proxy in front of the server. The reverse proxy
accepts HTTPS connections from browsers and proxies
plain HTTP to the server on the loopback interface or
across a private network. The reverse proxy also serves
the static web client assets that the build process
emits into `client/dist/`.

The recommended topology has the following properties:

- The reverse proxy terminates TLS for browser
  connections and the MCP transport.
- The reverse proxy redirects HTTP requests to HTTPS so
  that no credential ever travels over plain HTTP.
- The reverse proxy issues an HTTP Strict Transport
  Security (HSTS) header so browsers refuse to
  downgrade subsequent requests.
- The reverse proxy serves the static client bundle and
  proxies `/api`, `/mcp`, and `/health` to the Go server
  on port 8080.
- The Go server listens on plain HTTP on a port that is
  not exposed to the network; the reverse proxy is the
  only client that connects to the server directly.

The official Docker images follow this topology. The
client container runs nginx as a non-root user, serves
the bundled SPA, and proxies API and MCP traffic to the
server container. See the
[Docker deployment guide](../getting-started/docker.md)
for the production Compose configuration and the
[web client configuration page](../getting-started/configuration/client.md#nginx-configuration)
for an annotated nginx configuration.

## Operator Responsibilities

A reverse proxy alone does not satisfy the requirement;
the operator must also configure the proxy correctly.

- The proxy must present a valid TLS certificate
  issued by a certificate authority the browser
  trusts.
- The proxy must redirect every plain HTTP request to
  the corresponding HTTPS URL with a `301` or `308`
  response.
- The proxy must set the `Strict-Transport-Security`
  response header with a `max-age` of at least
  `31536000` seconds (one year).
- The proxy must forward the `X-Forwarded-For` and
  `X-Forwarded-Proto` headers so the server records
  the original client IP and scheme.
- The proxy must restrict access to the server's
  plain-HTTP listener; bind the listener to
  `127.0.0.1` or to a private interface that the proxy
  alone can reach.

The server's `http.trusted_proxies` list pairs with
the operator's network controls; configure the list
with the CIDR ranges of the reverse proxies that send
`X-Forwarded-For` headers. See the
[server configuration reference](../getting-started/configuration/server.md#http-server-http)
for details.

## Direct TLS on the Server

The Go server can terminate TLS directly when an
operator chooses not to deploy a reverse proxy. The
direct TLS path remains supported for environments
where a reverse proxy is impractical, such as
single-host appliance deployments or short-lived test
deployments behind a corporate VPN.

Configure direct TLS through the `http.tls` section of
`ai-dba-server.yaml`:

```yaml
http:
  address: ":8443"
  tls:
    enabled: true
    cert_file: "/etc/pgedge/certs/cert.pem"
    key_file: "/etc/pgedge/certs/key.pem"
    chain_file: "/etc/pgedge/certs/chain.pem"
```

Operators who terminate TLS on the server must still
serve the static web client bundle from a separate web
server because the Go server is API-only. The Go
server does not redirect plain HTTP to HTTPS and does
not emit an HSTS header; the operator must rely on
network controls to prevent plain-HTTP access to the
server's listener.

The recommended topology remains the reverse proxy in
front of the server. Direct TLS is the exception
rather than the rule.

## The Vite Development Server

The Vite development server at `http://localhost:5173`
is a developer-only tool. The development server runs
on plain HTTP, lacks the production hardening of the
reverse proxy, and includes hot-reload tooling that is
unsuitable for any shared environment. The development
server is supported only for local development on the
loopback interface.

Do not expose port 5173 to a network of any kind. The
development server transmits credentials over plain
HTTP and provides no path to enable TLS. Deploy the
production client bundle behind a reverse proxy
instead, as described above.

## Risks of Ignoring This Requirement

A deployment that exposes the workbench over plain
HTTP exposes every credential the user enters or the
client sends. The concrete risks include the
following items.

- A passive attacker on the network path can capture
  the user's password during login.
- A passive attacker can capture a session token from
  the `Authorization` header of any subsequent
  request.
- A passive attacker can capture API tokens that
  scripts send through the REST API or the MCP
  endpoint.
- An active attacker can inject responses, replace
  the SPA bundle, or intercept multi-factor
  challenges.

The workbench treats reports of plain-HTTP credential
exposure on the development server as configuration
issues rather than vulnerabilities, because the
development server is documented as localhost-only.
Reports of plain-HTTP credential exposure on a
production deployment indicate a missing or
misconfigured reverse proxy; review the topology
above and the
[Docker deployment guide](../getting-started/docker.md)
to restore a supported configuration.

## See Also

- The
  [installation guide](../getting-started/installation.md)
  covers the standard production deployment.
- The
  [Docker deployment guide](../getting-started/docker.md)
  documents the container topology that ships with
  the project.
- The
  [server configuration reference](../getting-started/configuration/server.md#http-server-http)
  lists every option for the `http` and `http.tls`
  sections.
- The
  [web client configuration page](../getting-started/configuration/client.md#nginx-configuration)
  shows an annotated nginx configuration for the
  static SPA.
