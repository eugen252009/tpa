#!/usr/bin/env python3
"""Estimate whole-file content-addressed reuse between two repository trees."""
import hashlib
import os
import sys


def inventory(root):
    out = []
    for base, dirs, files in os.walk(root):
        dirs.sort()
        for name in sorted(files):
            path = os.path.join(base, name)
            rel = os.path.relpath(path, root).replace(os.sep, "/")
            digest = hashlib.sha256()
            size = 0
            with open(path, "rb") as stream:
                for block in iter(lambda: stream.read(1024 * 1024), b""):
                    size += len(block)
                    digest.update(block)
            out.append((rel, size, digest.hexdigest()))
    return out


if len(sys.argv) != 3:
    raise SystemExit("usage: compare-generations.py OLD_TREE NEW_TREE")
old_root, new_root = sys.argv[1:]
old = inventory(old_root)
new = inventory(new_root)
old_hashes = {digest for _, _, digest in old}
new_hashes = {digest for _, _, digest in new}
reusable = [(path, size, digest) for path, size, digest in new if digest in old_hashes]
new_bytes = sum(size for _, size, _ in new)
reuse_bytes = sum(size for _, size, _ in reusable)
changed = [(path, size, digest) for path, size, digest in new if digest not in old_hashes]
old_paths = {path for path, _, _ in old}
new_paths = {path for path, _, _ in new}
print(f"old_files={len(old)}")
print(f"new_files={len(new)}")
print(f"new_generation_bytes={new_bytes}")
print(f"new_files_with_sha256_already_present={len(reusable)}")
print(f"new_bytes_reusable_by_whole_file_hash={reuse_bytes}")
print(f"new_files_requiring_upload={len(changed)}")
print(f"new_bytes_requiring_upload={sum(size for _, size, _ in changed)}")
print(f"estimated_bytes_saved_percent={100 * reuse_bytes / new_bytes:.4f}" if new_bytes else "estimated_bytes_saved_percent=0")
print(f"old_paths_removed={len(old_paths - new_paths)}")
print(f"new_paths_added={len(new_paths - old_paths)}")
