# LORISS

Go service scaffold for calendar synchronization. The HTTP service currently
provides only a liveness endpoint; authentication and synchronization are not
implemented. PostgreSQL can run alongside the Go container using Compose.

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
the image. The application consumes `PORT` and optional `DATABASE_URL`.

On SIGTERM or interrupt, the service allows active requests up to 10 seconds
to finish. Give the container at least 15 seconds to stop before forcing it.

## Run with PostgreSQL

Copy `.env.example` to `.env` and set `POSTGRES_PASSWORD` to a random password
of at least 32 alphanumeric characters. Then start both containers:

```sh
docker compose up --detach --build
```

Compose waits for PostgreSQL to accept connections before starting the app.
The Go container receives `DATABASE_URL` pointing to `db:5432` on their shared
network and uses the existing PostgreSQL store to check the connection once.
The pool closes during server shutdown. HTTP can still run without
`DATABASE_URL`, or if the configured database is temporarily unavailable.
`/healthz` retains its existing HTTP-only meaning.

PostgreSQL 17 stores data in the named `postgres_data` volume. `docker compose
down` retains it; `docker compose down --volumes` deletes it. PostgreSQL is not
published to the host by default. To enable localhost port 5432 for the existing
migration command or database tools:

```sh
docker compose -f compose.yaml -f compose.db-port.yaml up --detach
```

Run the existing migration command manually to create the application tables.
In PowerShell, replace `YOUR_PASSWORD` with the password from `.env`:

```powershell
$env:DATABASE_URL = 'postgres://loriss:YOUR_PASSWORD@127.0.0.1:5432/loriss?sslmode=disable'
go run ./cmd/migrate
```

Use `db` as the hostname inside Compose and `127.0.0.1` from the host. Keep
passing the override file when you want its host port mapping. No migrations
run automatically. The database bootstrap credentials and `sslmode=disable`
are for local development; use platform secrets and verified TLS in deployment.
Changing `.env` does not rotate the password in an existing database volume.
Synchronization handlers and workers are not implemented by this container setup.

## Checks

```sh
go test ./...
go vet ./...
```
