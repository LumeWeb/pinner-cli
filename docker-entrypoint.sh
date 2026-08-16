#!/bin/sh
set -eu

# Pinner Docker entrypoint — thin delegating wrapper.
#
# The CLI resolves most MCP_* configuration natively from the environment
# (flags declare `Sources: cli.EnvVars(...)` in the source). This shell adds the
# one bridge that the CLI does not supply: the PaaS-universal `$PORT` convention.
# The MCP HTTP transport reads `MCP_PORT` for its bind port, but Railway, Koyeb,
# Fly, Render, Cloud Run, and DO all inject `$PORT`. If the operator set
# `MCP_PORT` explicitly it wins; otherwise we map `$PORT` (default 8080) onto
# `MCP_PORT` so the container binds the platform-injected port on 0.0.0.0.
#
# Default command:
#   * `docker run ...`                          -> `pinner mcp --http`
#       (streamable-HTTP MCP on 0.0.0.0:$PORT, endpoint /mcp — PaaS/registry default)
#   * `docker run ... <any pinner args>`        -> passed through verbatim
#       (e.g. `pinner mcp` for stdio, or any CLI subcommand)

# Bridge the PaaS $PORT convention onto the CLI's MCP_PORT if not already set.
if [ -z "${MCP_PORT:-}" ]; then
    export MCP_PORT="${PORT:-8080}"
fi

if [ "$#" -gt 0 ]; then
    # Any provided args are a pinner invocation — pass through verbatim.
    exec pinner "$@"
fi

# No args: serve the streamable-HTTP MCP transport (the PaaS/registry default).
exec pinner mcp --http
