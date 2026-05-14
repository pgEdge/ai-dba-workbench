# Web Client Configuration

The pgEdge AI DBA Workbench web client uses Vite as
the build tool and development server. Configuration
covers the development proxy, build settings, and
runtime behavior.

## Development Server

The Vite development server starts on port 5173 by
default. Start the development server with the
following command:

```bash
cd client
npm run dev
```

The application is available at
`http://localhost:5173` after the server starts.

!!! warning "Localhost only"
    The Vite development server is a developer-only
    tool that runs on plain HTTP and is supported only
    on the loopback interface. Do not expose port
    `5173` to any network; the development server
    transmits credentials in cleartext and offers no
    TLS option. For any network-accessible deployment,
    build the production bundle and front it with a
    TLS-terminating reverse proxy as described in the
    [TLS and reverse proxy requirements](../../admin-guide/tls-and-reverse-proxy.md).

## API Proxy Configuration

The development server proxies all API requests to the
MCP server. By default, the proxy forwards requests
matching the `/api` path prefix to
`http://localhost:8080`.

The proxy configuration is defined in the
`vite.config.ts` file. In the following example, the
configuration shows the default proxy settings:

```typescript
server: {
    port: 5173,
    proxy: {
        '/api': {
            target: 'http://localhost:8080',
            changeOrigin: true,
            cookieDomainRewrite: '',
        },
    },
},
```

### Changing the Server Port

If the MCP server runs on a different port, update the
`target` value in the proxy configuration. In the
following example, the proxy forwards requests to port
9090:

```typescript
proxy: {
    '/api': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        cookieDomainRewrite: '',
    },
},
```

### Cookie Forwarding

The proxy automatically forwards cookies between the
browser and the MCP server. The `cookieDomainRewrite`
option ensures cookies work correctly across the proxy
boundary. The `changeOrigin` option rewrites the
request origin header to match the target server.

## Build Configuration

Create a production build with the following command:

```bash
cd client
npm run build
```

The build process generates optimized static files in
the `dist` directory. The build configuration is
defined in the `vite.config.ts` file.

In the following example, the configuration shows the
default build settings:

```typescript
build: {
    outDir: 'dist',
    sourcemap: true,
},
```

### Output Directory

The `outDir` option specifies the directory for the
production build output. The default value is `dist`
relative to the client project root.

### Source Maps

The `sourcemap` option enables source map generation
for the production build. Source maps help with
debugging deployed applications. Set this option to
`false` to disable source maps in production.

## Production Deployment

For production deployments, serve the built files from
the `dist` directory using a web server such as Nginx
or Apache. The MCP server does not serve the web
client files; a separate web server is required.

!!! warning "TLS is required"
    Any network-accessible deployment must terminate
    TLS in front of the server. The reverse proxy is
    responsible for TLS termination, HTTP-to-HTTPS
    redirection, and HSTS. See the
    [TLS and reverse proxy requirements](../../admin-guide/tls-and-reverse-proxy.md)
    for the full operator checklist.

### Nginx Configuration

The following example targets a non-containerized
deployment in which Nginx runs on the host as the
root user and serves the built client files directly
from disk. The official client container image runs
Nginx as a non-root user and listens on port 8080;
see the Docker deployment guide for the container
configuration.

In the following example, the Nginx configuration
serves the web client and proxies API, MCP, and
health check requests to the server:

```nginx
server {
    listen 80;
    server_name workbench.example.com;

    root /opt/ai-workbench/client;
    index index.html;

    location /api/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For
            $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto
            $scheme;
    }

    location /mcp/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For
            $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto
            $scheme;

        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 300s;
    }

    location = /health {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For
            $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto
            $scheme;
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

The `/mcp/` location disables proxy buffering and
caching because MCP requests may use server-sent
events or streaming responses. The `/health`
location proxies health check requests to the
server. The `try_files` directive ensures that
client-side routing works correctly by falling back
to `index.html` for all unmatched paths.

## Theme Settings

The web client supports light and dark themes. The
theme selection persists across browser sessions using
local storage. Users can toggle the theme through the
application interface.

## Prerequisites

The web client requires the following tools for
development:

- [Node.js 18](https://nodejs.org/) or later.
- [npm 9](https://docs.npmjs.com/) or later.

Install the project dependencies before starting
development:

```bash
cd client
npm install
```

## Testing

Run the test suite with the following command:

```bash
npm test
```

Run tests in watch mode for interactive development:

```bash
npm run test:watch
```

Generate a coverage report with the following command:

```bash
npm run test:coverage
```
