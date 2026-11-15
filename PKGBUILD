# Maintainer: Agent V <agent-v@kilo.local>
pkgname=dm
pkgver=0.2.1.r1.g0000000
pkgrel=1
pkgdesc="Lightweight segmented download manager: local daemon, GTK4 client and browser host"
arch=('x86_64' 'aarch64')
url="https://github.com/leodas66/DM"
license=('MIT')
depends=('glibc' 'gtk4')
makedepends=('git' 'curl')
source=("dm::git+https://github.com/leodas66/DM.git#branch=main")
sha256sums=('SKIP')

pkgver() {
  cd "${srcdir}/dm"
  local version commit_epoch short_sha
  version="$(cat VERSION)"
  commit_epoch="$(git log -1 --format=%ct)"
  short_sha="$(git rev-parse --short=7 HEAD)"
  [[ "$version" && "$commit_epoch" =~ ^[0-9]+$ && "$short_sha" =~ ^[0-9a-f]{7,}$ ]] || return 1
  printf '%s.r%s.g%s' "$version" "$commit_epoch" "$short_sha"
}

prepare() {
  cd "${srcdir}/dm"
  local asset expected bundled_sha commit_sha
  asset="dm-linux-${CARCH}.tar.gz"
  commit_sha="$(git rev-parse --short=7 HEAD)"

  # Fetch the rolling assets on every source refresh instead of letting
  # makepkg's SRCDEST cache an old archive under a permanent filename.
  curl -fsSL "https://github.com/leodas66/DM/releases/download/continuous/$asset" \
    -o "${srcdir}/$asset"
  curl -fsSL "https://github.com/leodas66/DM/releases/download/continuous/SHA256SUMS" \
    -o "${srcdir}/SHA256SUMS"

  expected="$(awk -v asset="$asset" '$2 == asset {print $1}' "${srcdir}/SHA256SUMS")"
  [[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || {
    error "No valid checksum found for $asset in SHA256SUMS"
    return 1
  }
  printf '%s  %s\n' "$expected" "${srcdir}/$asset" | sha256sum --check --status || {
    error "Checksum mismatch for $asset"
    return 1
  }

  bundled_sha="$(tar -xOzf "${srcdir}/$asset" BUILD_INFO | awk -F= '$1 == "SHORT_SHA" {print $2}')"
  [[ "$bundled_sha" == "$commit_sha" ]] || {
    error "The continuous release does not match the latest main commit yet; retry after its build completes."
    return 1
  }
}

package() {
  tar -xzf "${srcdir}/dm-linux-${CARCH}.tar.gz" -C "$pkgdir"
}
