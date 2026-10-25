# Building and testing from a source checkout

## Build from source

```sh
git clone https://github.com/noisefloor-oss/sdlc.git
cd sdlc
git checkout v0.4.1        # or the latest release tag
go build -o noise-sdlc ./cmd/noise-sdlc
./noise-sdlc --version     # verify the build runs
```

Move the binary anywhere on your `PATH`, for example
`install -m 0755 noise-sdlc ~/.local/bin/`. Running
`go install ./cmd/noise-sdlc` from the clone works too.

## Test

```sh
go build ./...
go vet ./...
go test ./...
```

The binary normally uses its embedded resources. To test catalog or schema
edits without rebuilding, point `NOISE_SDLC_SOURCE_ROOT` at a source
checkout, for example `NOISE_SDLC_SOURCE_ROOT="$PWD" noise-sdlc check-catalog`;
missing or invalid files there are errors, never a silent fallback. The
repository-local launcher `./bin/noise-sdlc` rebuilds from current source
when its freshness receipt does not match, so it always runs what is checked
out.

