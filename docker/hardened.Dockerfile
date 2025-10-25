# syntax=docker/dockerfile:1.4

# Base image (pinned digest for reproducibility)
ARG BASE_IMAGE_DIGEST=sha256:649032f4044328926c94f52cb1a9687e94576a7836b3a8bc3316b1abaa44d953
FROM factoriotools/factorio@${BASE_IMAGE_DIGEST} AS base

# Upstream image already includes 'factorio' user and entrypoint
USER root

# Init stage: prepare default Factorio configuration files
FROM busybox:1.36 AS init-config
WORKDIR /defaults/config

RUN set -eux; \
    mkdir -p /defaults/config && \
    echo "[path]" > /defaults/config/config.ini && \
    echo "read-data=/opt/factorio/data" >> /defaults/config/config.ini && \
    echo "write-data=/factorio" >> /defaults/config/config.ini

# Hardened runtime stage
FROM base

# Copy default configuration files from init stage
COPY --chown=factorio:factorio --from=init-config /defaults/config /factorio/config/

# Prepare Factorio runtime directories and redirect writable files
USER root
RUN set -eux; \
    mkdir -p /factorio/config /factorio/saves /factorio/mods /factorio/scenarios /factorio/script-output /factorio/logs /factorio/temp; \
    echo '{"mods":[]}' > /factorio/mods/mod-list.json; \
    chown -R factorio:factorio /factorio; \
    chmod -R 750 /factorio; \
    if [ -d /opt/factorio ]; then \
        # Lock file
        rm -f /opt/factorio/.lock || true; \
        ln -sf /factorio/.lock /opt/factorio/.lock; \
        # Redirect entire log directory to writable /factorio/logs
        rm -rf /opt/factorio/logs || true; \
        ln -sf /factorio/logs /opt/factorio/logs; \
        # Ensure empty writable log files exist
        touch /factorio/logs/factorio-current.log; \
        touch /factorio/logs/factorio-previous.log; \
        # Make logs world-readable for K8s log collectors
        chmod 644 /factorio/logs/factorio-current.log /factorio/logs/factorio-previous.log; \
        # Temp and mods
        rm -rf /opt/factorio/temp || true; \
        ln -sf /factorio/temp /opt/factorio/temp; \
        rm -rf /opt/factorio/mods || true; \
        ln -sf /factorio/mods /opt/factorio/mods; \
        # Player data
        rm -f /opt/factorio/player-data.json || true; \
        ln -sf /factorio/player-data.json /opt/factorio/player-data.json; \
        # Ensure top-level log files also point to writable targets
        rm -f /opt/factorio/factorio-current.log /opt/factorio/factorio-previous.log || true; \
        ln -sf /factorio/logs/factorio-current.log /opt/factorio/factorio-current.log; \
        ln -sf /factorio/logs/factorio-previous.log /opt/factorio/factorio-previous.log; \
    fi

# Replace entrypoint with hardened wrapper
RUN mv /docker-entrypoint.sh /docker-entrypoint.orig.sh && \
    # Disable write-data mutation in the original entrypoint (read-only safe)
    sed -i 's|sed -i .*config.ini.*|echo "[FACTORIO-HARDENED] Skipping write-data modification"|g' /docker-entrypoint.orig.sh && \
    cat >/docker-entrypoint.sh <<'SCRIPT'
#!/bin/sh
set -e
echo "[FACTORIO-HARDENED] Wrapper entrypoint active"
echo "[FACTORIO-HARDENED] Redirecting logs via --console-log /factorio/logs/factorio-current.log"

# Ensure writable directories exist (in case volumes are empty)
for d in /factorio/config /factorio/saves /factorio/mods /factorio/scenarios /factorio/script-output /factorio/logs /factorio/temp; do
    mkdir -p "$d"
done

# Ensure mod-list.json exists before DLC loader runs
if [ ! -f /factorio/mods/mod-list.json ]; then
    echo '{"mods":[]}' > /factorio/mods/mod-list.json
fi

# Run Factorio with redirected log output
exec /opt/factorio/bin/x64/factorio --console-log /factorio/logs/factorio-current.log "$@"
SCRIPT
RUN chmod +x /docker-entrypoint.sh

# Drop privileges for runtime
USER factorio:factorio

# Explicit writable volumes for read-only root filesystem
VOLUME ["/factorio/config", "/factorio/saves", "/factorio/mods", "/factorio/scenarios", "/factorio/script-output", "/factorio/logs", "/factorio/temp"]

WORKDIR /factorio

# Security metadata and labels
LABEL org.opencontainers.image.title="Factorio Hardened"
LABEL org.opencontainers.image.description="Security-hardened Factorio image for read-only and non-root environments."
LABEL org.opencontainers.image.security.cap-drop="ALL"
LABEL org.opencontainers.image.security.no-new-privileges="true"
LABEL org.opencontainers.image.base.digest="${BASE_IMAGE_DIGEST}"

ENV DOCKER_SECURITY_OPTS="no-new-privileges:true"
ENV SEC_COMP_PROFILE="RuntimeDefault"

# Health check for UDP port 34197
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
  CMD nc -z 127.0.0.1 34197 || exit 1

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["--start-server", "save.zip"]
