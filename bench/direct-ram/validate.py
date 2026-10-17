#!/usr/bin/env python3
"""Untimed independent archive/semantic oracle for compare-direct.py (stdlib + dpkg).
Checks EVERY control archive, script mode/body and empty data archive. Uses dpkg-deb
on deterministic boundary samples; does not pretend all packages were installed.
"""
import hashlib
import io
import json
import pathlib
import subprocess
import tarfile


def members(data):
    if data[:8] != b"!<arch>\n":
        raise ValueError("not a Debian ar archive")
    pos, result = 8, {}
    while pos < len(data):
        h = data[pos:pos + 60]
        if len(h) != 60 or h[58:] != b"`\n":
            raise ValueError("invalid ar header")
        name = h[:16].decode().strip().rstrip("/")
        size = int(h[48:58])
        if size < 0 or pos + 60 + size > len(data) or name in result:
            raise ValueError("invalid ar member")
        result[name] = data[pos + 60:pos + 60 + size]
        pos += 60 + size + size % 2
    return result


def control(text):
    fields, key = {}, None
    for line in text.splitlines():
        if line.startswith((" ", "\t")) and key:
            fields[key] += "\n" + line
        elif line:
            key, value = line.split(":", 1)
            if key in fields:
                raise ValueError("duplicate control field")
            fields[key] = value.strip()
    return fields


def validate(repo, count):
    files = sorted(pathlib.Path(repo).rglob("*.deb"))
    assert len(files) == count
    digest = hashlib.sha256()
    samples = sorted({0, len(files)//2, len(files)-1})
    package_bytes = 0
    for i, path in enumerate(files):
        data = path.read_bytes(); package_bytes += len(data)
        ar = members(data)
        assert ar.pop("debian-binary") == b"2.0\n"
        assert len(ar) == 2
        semantic = {}
        for name, archive in ar.items():
            assert name.startswith(("control.tar", "data.tar"))
            with tarfile.open(fileobj=io.BytesIO(archive), mode="r:*") as tar:
                for entry in tar:
                    if entry.isdir():
                        continue
                    assert entry.isfile(), entry.name
                    assert name.startswith("control.tar"), "unexpected payload"
                    filename = entry.name.removeprefix("./")
                    assert filename not in semantic
                    assert entry.uid == entry.gid == 0
                    body = tar.extractfile(entry).read()
                    if filename == "control":
                        assert entry.mode == 0o644
                        semantic[filename] = control(body.decode())
                    else:
                        assert filename in {"preinst", "postinst", "prerm", "postrm"}
                        assert entry.mode == 0o755
                        assert body == b"#!/bin/sh\nset -e\n"
                        semantic[filename] = body.decode()
        assert set(semantic) == {"control", "preinst", "postinst", "prerm", "postrm"}
        c = semantic['control']
        assert c['Package'] == f'meta-{i:05d}' and c['Version'] == '1.0'
        assert c['Architecture'] == 'all' and c['Depends'] == 'dpkg (>= 1.0)'
        digest.update(json.dumps(semantic, sort_keys=True).encode() + b'\n')
        if i in samples:
            independent = subprocess.check_output(['dpkg-deb', '-f', str(path)], text=True)
            assert control(independent) == c
            subprocess.run(['dpkg-deb', '--contents', str(path)], check=True, stdout=subprocess.DEVNULL)
    return {'semantic_sha256': digest.hexdigest(), 'archives_checked': count,
            'dpkg_sample_indices': samples, 'package_bytes': package_bytes,
            'semantic_scope': 'all control fields, four script bodies/modes, root ownership, empty payload; archive timestamps/compression excluded'}
