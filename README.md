# LORISS

Go service scaffold for calendar synchronization. The HTTP service currently
provides only a liveness endpoint; authentication and synchronization are not
implemented. No database is required or included in this container setup.

## Run locally

Install Go 1.26 or later, then run:

```sh
go run ./cmd/server
```

`PORT` sets the listening port (default: `8080`, allowed range: `1–65535`). The
server listens on all interfaces. `GET /healthz` returns HTTP 200 with
`{"status":"ok"}`. This indicates that HTTP is running, not that integrations
are configured or ready.

## Build and run the container

Start Docker with Linux containers enabled, then run from the repository root:

```sh
docker build -t loriss:local .
docker run --detach --name loriss --publish 127.0.0.1:8080:8080 --stop-timeout 15 loriss:local
curl http://localhost:8080/healthz
docker logs loriss
docker stop loriss
docker rm loriss
```

In Windows PowerShell, use `curl.exe` for the health check. The image compiles
the service in a Go builder stage and runs the static binary as a non-root user
in a minimal image containing CA certificates for outbound HTTPS. Its runtime
has no shell. Configure deployment health probes to request `/healthz` on the
configured port.

To use another port, replace the run command with:

```sh
docker run --detach --name loriss --env PORT=9090 --publish 127.0.0.1:9090:9090 --stop-timeout 15 loriss:local
```

Supply future integration configuration through runtime environment variables
or your deployment platform's secret mechanism. Do not bake credentials into
the image. Only `PORT` is currently consumed by the application.

On SIGTERM or interrupt, the service allows active requests up to 10 seconds
to finish. Give the container at least 15 seconds to stop before forcing it.

## Checks

```sh
go test ./...
go vet ./...
```
