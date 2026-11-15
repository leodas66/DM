# Maintainer: Agent V <agent-v@kilo.local>
pkgname=dm
pkgver=0.2.1.r1.g0000000
pkgrel=1
pkgdesc="Lightweight segmented download manager: local daemon, GTK4 client and browser host"
arch=('x86_64' 'aarch64')
url="https://github.com/leodas66/DM"
license=('MIT')
depends=('glibc' 'gtk4')
source=(
  "dm-linux-${CARCH}.tar.gz::https://github.com/leodas66/DM/releases/download/continuous/dm-linux-${CARCH}.tar.gz"
  "SHA256SUMS::https://github.com/leodas66/DM/releases/download/continuous/SHA256SUMS"
)
noextract=("dm-linux-${CARCH}.tar.gz")
sha256sums=('SKIP' 'SKIP')

pkgver() {
  local info version build_number short_sha
  info="$(tar -xOzf "${srcdir}/dm-linux-${CARCH}.tar.gz" BUILD_INFO)" || return 1
  version="$(awk -F= '$1 == "VERSION" {print $2}' <<< "$info")"
  build_number="$(awk -F= '$1 == "BUILD_NUMBER" {print $2}' <<< "$info")"
  short_sha="$(awk -F= '$1 == "SHORT_SHA" {print $2}' <<< "$info")"
  [[ "$version" && "$build_number" =~ ^[0-9]+$ && "$short_sha" =~ ^[0-9a-f]{7}$ ]] || return 1
  printf '%s.r%s.g%s' "$version" "$build_number" "$short_sha"
}

prepare() {
  local asset expected
  asset="dm-linux-${CARCH}.tar.gz"
  expected="$(awk -v asset="$asset" '$2 == asset {print $1}' "${srcdir}/SHA256SUMS")"
  [[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || {
    error "No valid checksum found for $asset in SHA256SUMS"
    return 1
  }
  printf '%s  %s\n' "$expected" "${srcdir}/$asset" | sha256sum --check --status || {
    error "Checksum mismatch for $asset"
    return 1
  }
}

package() {
  tar -xzf "${srcdir}/dm-linux-${CARCH}.tar.gz" -C "$pkgdir"
}
