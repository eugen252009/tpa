#!/bin/bash

ARCHS=("amd64" "riscv64" "arm64")
DESC="$(cat ./description.txt)"

mkdir -p "dist"
if [ ! -f "./manpage/usr/share/man/man1/tpa.1.gz" ]; then
	gzip ./manpage/usr/share/man/man1/tpa.1
fi

for arch in "${ARCHS[@]}"
do
    echo "Building for $arch..."
    WORK_DIR="tpa-$arch"
    mkdir -p "dist/$WORK_DIR"
    mkdir -p "$WORK_DIR/usr/share/man/man1"
    mkdir -p "$WORK_DIR/usr/local/bin"

    ./tpa init -name=tpa -ver=0.1 -depends="libc6,dpkg,gpg,gzip" -desc="$DESC" -homepage="https://github.com/coffeemakerstudio/tpa"-maintainer="Coffee Maker Studio <tpa@lupricht.net>" -section=utils -out="$WORK_DIR" -arch="$arch" 
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$WORK_DIR/usr/local/bin/tpa" .
    cp manpage/usr/share/man/man1/tpa.1.gz "$WORK_DIR/usr/share/man/man1/"
    
    ./tpa build -in="$WORK_DIR" -out="dist/$WORK_DIR/tpa-$arch.deb"

	rm -rf "$WORK_DIR"
done

echo "Building done"
