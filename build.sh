#!/bin/bash

ARCHS=("amd64" "riscv64" "arm64")
DESC="$(cat ./description.txt)"
VERSION=0.1
DEPENDS="libc6,dpkg,gpg,gzip"
HOMEPAGE="https://github.com/coffeemakerstudio/tpa"
MAINTAINER="Coffee Maker Studio <tpa@lupricht.net>"
SECTION="utils"

if [ ! -f "./tpa" ]; then
	echo "building tpa before building packages"
	go build -o tpa . 
fi
if [ ! -f "./manpage/usr/share/man/man1/tpa.1.gz" ]; then
	gzip --keep ./manpage/usr/share/man/man1/tpa.1
fi

mkdir -p "dist"
for arch in "${ARCHS[@]}"
do
    echo "Building for $arch..."
    WORK_DIR="tpa-$arch"
	OUT_DIR="dist"
    mkdir -p "dist"
    mkdir -p "$WORK_DIR/usr/share/man/man1"
    mkdir -p "$WORK_DIR/usr/local/bin"

    ./tpa init \
		-name=tpa \
		-ver="$VERSION" \
		-depends="$DEPENDS" \
		-desc="$DESC" \
		-homepage="$HOMEPAGE" \
		-maintainer="$MAINTAINER" \
		-section="$SECTION" \
		-out="$WORK_DIR" \
		-arch="$arch" 

	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$WORK_DIR/usr/local/bin/tpa" .
    cp manpage/usr/share/man/man1/tpa.1.gz "$WORK_DIR/usr/share/man/man1/"
    
    ./tpa build -in="$WORK_DIR" -out="$OUT_DIR/tpa-$arch.deb"

	rm -rf "$WORK_DIR"
done

echo "Building done"
