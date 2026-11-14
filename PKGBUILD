# Maintainer: Agent V <agent-v@kilo.local>
pkgname=dm
pkgver=0.2.0
pkgrel=1
pkgdesc="Lightweight segmented download manager: local daemon, GTK4 client and browser host"
arch=('x86_64' 'aarch64')
url="https://github.com/vishwa/dm"
license=('MIT')
depends=('glibc' 'gtk4')
makedepends=('go' 'gtk4' 'git')
source=("git+https://github.com/vishwa/dm.git#tag=v${pkgver}")
sha256sums=('SKIP')

build() {
  cd dm

  # The daemon is a pure-Go binary; the client needs cgo for GTK4.
  export CGO_ENABLED=1
  export GOFLAGS="-trimpath"

  go build -o dm ./cmd/dm
  go build -o dm-native-host ./cmd/dm-native-host
  (cd desktop && go build -o dm-desktop .)
}

package() {
  cd dm

  install -Dm755 dm "$pkgdir/usr/bin/dm"
  install -Dm755 dm-native-host "$pkgdir/usr/bin/dm-native-host"
  install -Dm755 desktop/dm-desktop "$pkgdir/usr/bin/dm-desktop"

  install -Dm644 dm.service "$pkgdir/usr/lib/systemd/user/dm.service"
  install -Dm644 packaging/io.github.vishwa.DM.desktop \
    "$pkgdir/usr/share/applications/io.github.vishwa.DM.desktop"
  install -Dm644 packaging/io.github.vishwa.DM.svg \
    "$pkgdir/usr/share/icons/hicolor/scalable/apps/io.github.vishwa.DM.svg"

  # Firefox picks up a system-wide Native Messaging manifest. Chrome needs the
  # extension ID, which is unknown until the extension is installed, so that
  # manifest is written per user (see README.md).
  install -Dm644 packaging/io.github.vishwa.dm.firefox.json \
    "$pkgdir/usr/lib/mozilla/native-messaging-hosts/io.github.vishwa.dm.json"

  install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
