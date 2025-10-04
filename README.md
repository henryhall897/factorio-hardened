# 🏭 Factorio Hardened

A custom, hardened Docker image for running **Factorio** on Kubernetes clusters with secure policies enabled.

This project forks and customizes the upstream [`factoriotools/factorio`](https://hub.docker.com/r/factoriotools/factorio) Docker image to:
- Comply with Kubernetes **baseline/restricted** security policies (Kyverno, OPA, Pod Security Standards)
- Run as **non-root** where possible
- Use **pinned digests** for reproducibility
- Redirect writable paths to mounted volumes (PVCs and `emptyDir`s)
- Support **`readOnlyRootFilesystem: true`** and minimal Linux capabilities
- Provide a production-ready Factorio server container for **homelabs** and **secure clusters**

> ⚠️ This repo is not affiliated with or endorsed by Wube Software or Factorio Tools. It’s a community build for educational and self-hosting purposes.

---

## 🎯 Purpose

Running game servers like Factorio inside Kubernetes clusters that enforce **secure pod policies** (Kyverno, PSA baseline/restricted, image hygiene) can be challenging.  
The upstream Factorio image expects a fully writable root filesystem and root privileges.

This repo’s goal is to **repackage and harden Factorio** so it works seamlessly in secure environments without sacrificing maintainability.

---

## 🔧 Features

- Hardened `Dockerfile` based on upstream Factorio image
- Non-root execution with a dedicated UID/GID
- Configurable volumes for saves, mods, and config
- Helm chart compatibility (PVCs + ConfigMaps)
- GitHub Actions CI/CD for automatic builds and pushes to GitHub Container Registry (GHCR)

---

## 📝 Requirements

### Build Requirements
- [Docker](https://docs.docker.com/) (or Podman) installed locally
- [Git](https://git-scm.com/)
- Access to a container registry (e.g., GitHub Container Registry, Docker Hub)
- Optional: [docker-compose](https://docs.docker.com/compose/) for local testing
- Optional: [Hadolint](https://github.com/hadolint/hadolint) and [Trivy](https://aquasecurity.github.io/trivy/) for linting/scanning images

### Kubernetes Requirements
- Kubernetes v1.25+ (K3s or standard)
- Helm v3 for deployment
- A PVC for Factorio saves and mods
- Kyverno or PSA baseline/restricted policies configured (this image is built to comply)

---

## 📦 Using This Image

Once built or pulled, deploy Factorio with your Helm chart by updating:

```yaml
image:
  repository: ghcr.io/<yourusername>/factorio-hardened
  tag: latest
  pullPolicy: IfNotPresent
