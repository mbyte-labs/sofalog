# Releasing and the Docker image

The `Dockerfile` is multi-stage — Node builds the SPA, Go compiles the binary around it, and only
the binary lands in an Alpine runtime image. `docker-compose.yml` runs it with `./data` bind-mounted
and no secret written into the file.

## The published image

[`.github/workflows/docker.yml`](../.github/workflows/docker.yml) builds the Dockerfile on GitHub
Actions for `linux/amd64` and `linux/arm64` and publishes it to GHCR as
`ghcr.io/mbyte-labs/sofalog`. The compose file names that image, so plain `docker compose up -d`
pulls it.

A release is a `vX.Y.Z` git tag whose commit is on `main`: the workflow writes `latest`, `X.Y.Z`
and `X.Y` for it, and refuses a tag cut from any other branch. Nothing is published on an ordinary
push.

```bash
git tag v0.1.0 && git push origin v0.1.0
```

To get an image of an unreleased branch, run the workflow by hand from the Actions tab
(Docker image → Run workflow, pick the branch); that pushes `<branch>` and `sha-<short sha>`, or
only checks that the Dockerfile builds if you untick "Push". Pull requests build the image but push
nothing, so a broken Dockerfile fails the PR.

GHCR packages are private by default, so an anonymous `docker pull` fails until the package is
made public once on GitHub: repository → **Packages** → **sofalog** → **Package settings** →
**Change visibility** → Public. Until then, pull with a token that has `read:packages`, or build
from source.

## Building from source

The compose file keeps its `build:` block, so `--build` compiles the current checkout and tags the
result with the same image name — the rest of the file does not care which one produced it:

```bash
docker compose up -d --build
```

Check what the environment resolved to before starting anything:

```bash
docker compose config
```

Without Docker, the production artifact is **one binary**. The frontend is built first so the Go
build can embed it (`server/web/dist` is embedded via `go:embed`):

```bash
cd frontend && npm run build
cd ../server && go build .
```

Then run it (the binary is `server/sofalog`):

```bash
BASE_URL=https://media.example.com DATA_DIR=/var/lib/sofalog ./server/sofalog
```
