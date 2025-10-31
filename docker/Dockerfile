# syntax=docker/dockerfile:1.4

# --- Base image (pinned digest for reproducibility) ---
ARG BASE_IMAGE_DIGEST=sha256:bee8e1751f9cf59d673cb9eb751d55403407f90aa3701125b267ec778b85677a
FROM factoriotools/factorio@${BASE_IMAGE_DIGEST:-sha256:bee8e1751f9cf59d673cb9eb751d55403407f90aa3701125b267ec778b85677a} AS base

USER root

# --- Init stage: generate a minimal default config.ini ---
FROM busybox:1.36 AS init-config
WORKDIR /defaults/config
RUN set -eux; \
  mkdir -p /defaults/config && \
  { echo "[path]"; \
    echo "read-data=/opt/factorio/data"; \
    echo "write-data=/factorio"; } > /defaults/config/config.ini

# --- Hardened runtime stage ---
FROM base

# Copy default config to PVC path
COPY --chown=factorio:factorio --from=init-config /defaults/config /factorio/config/

# Create writable dirs inside /factorio (these map to PVC)
RUN set -eux; \
  mkdir -p /factorio/{config,saves,mods,scenarios,script-output,logs,temp} && \
  echo '{"mods":[{"name":"base","enabled":true},{"name":"elevated-rails","enabled":true},{"name":"quality","enabled":true},{"name":"space-age","enabled":true}]}' > /factorio/mods/mod-list.json && \
  chown -R factorio:factorio /factorio && chmod -R ug+rwX /factorio

# Copy in hardened entrypoint
COPY scripts/hardened-entrypoint.sh /usr/local/bin/hardened-entrypoint.sh
RUN chmod +x /usr/local/bin/hardened-entrypoint.sh
ENTRYPOINT ["/usr/local/bin/hardened-entrypoint.sh"]
CMD []
# --- Security hardening (core) ---
USER factorio:factorio
WORKDIR /factorio

LABEL org.opencontainers.image.title="Factorio Hardened"
LABEL org.opencontainers.image.description="Security-hardened Factorio image for Kubernetes (writable /opt/factorio handled by initContainer)."
LABEL org.opencontainers.image.security.cap-drop="ALL"
LABEL org.opencontainers.image.security.no-new-privileges="true"
LABEL org.opencontainers.image.base.digest="${BASE_IMAGE_DIGEST}"

ENV DOCKER_SECURITY_OPTS="no-new-privileges:true"
ENV SEC_COMP_PROFILE="RuntimeDefault"
ENV UPDATE_MODS_ON_START="false"

HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
  CMD nc -z 127.0.0.1 34197 || exit 1


