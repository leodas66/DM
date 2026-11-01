# Maintainer: Agent V <agent-v@kilo.local>
pkgname=dm
pkgver=0.1.0
pkgrel=1
pkgdesc="Lightweight segmented download manager daemon with web UI"
arch=('x86_64' 'aarch64')
url="https://github.com/vishwa/dm"
license=('MIT')
depends=('glibc')
makedepends=('go')
source=("git+https://github.com/vishwa/dm.git#tag=v${pkgver}")
sha256sums=('SKIP')

build() {
  cd dm
  go build -trimpath -o dm ./cmd/dm
}

package() {
  cd dm
  install -Dm755 dm "$pkgdir/usr/bin/dm"
  install -Dm644 -t "$pkgdir/usr/lib/systemd/user/" dm.service
  install -Dm644 -t "$pkgdir/usr/share/licenses/$pkgname/" LICENSE
}
