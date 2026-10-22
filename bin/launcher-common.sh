# Shared by the repository launcher and its supported prebuilt build command.
# Receipts establish repository-source freshness, not signed build provenance.

sdlc_go_command() {
    if [ -x /usr/local/go/bin/go ]; then
        printf '%s\n' /usr/local/go/bin/go
    else
        command -v go
    fi
}

sdlc_hash_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum < "$1"
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 < "$1"
    else
        return 1
    fi
}

sdlc_source_digest() (
    # Resources are embedded by default; NOISE_SDLC_SOURCE_ROOT explicitly
    # selects source files. Hash the root to bind the receipt to this checkout.
    cd "$repository_root" || exit 1
    [ -f go.mod ] && [ -d cmd/noise-sdlc ] && [ -d internal ] || exit 1
    printf '%s\n' "$repository_root" > "$build_directory/source-hashes" || exit 1
    printf '%s\0' go.mod > "$build_directory/source-list" || exit 1
    for input in go.sum go.work go.work.sum resources.go; do
        if [ -f "$input" ]; then
            printf '%s\0' "$input" >> "$build_directory/source-list" || exit 1
        fi
    done
    find -L cmd/noise-sdlc internal -type f -print0 >> "$build_directory/source-list" || exit 1
    for input in catalog docs/schemas; do
        if [ -d "$input" ]; then
            find -L "$input" -type f -print0 >> "$build_directory/source-list" || exit 1
        fi
    done
    if [ -d vendor ]; then
        find -L vendor -type f -print0 >> "$build_directory/source-list" || exit 1
    fi
    if command -v sha256sum >/dev/null 2>&1; then
        xargs -0 sha256sum -- < "$build_directory/source-list" >> "$build_directory/source-hashes" || exit 1
    elif command -v shasum >/dev/null 2>&1; then
        xargs -0 shasum -a 256 -- < "$build_directory/source-list" >> "$build_directory/source-hashes" || exit 1
    else
        exit 1
    fi
    LC_ALL=C sort "$build_directory/source-hashes" > "$build_directory/source-sorted" || exit 1
    result=$(sdlc_hash_file "$build_directory/source-sorted") || exit 1
    printf '%s\n' "${result%% *}"
)

sdlc_cleanup() {
    rm -rf -- "$build_directory"
}
