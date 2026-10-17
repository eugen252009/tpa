#!/usr/bin/env python3
"""Recompute the published historical table without running a benchmark."""
import json
import pathlib
import statistics

root = pathlib.Path(__file__).resolve().parents[1]
rows = json.loads((root / 'direct-pipeline/measurements.json').read_text())
assert len(rows) == 18
summary = []
for count in (100, 1000, 10000):
    modes = {}
    for mode in ('existing', 'direct'):
        trials = [r for r in rows if r['packages'] == count and r['mode'] == mode]
        assert sorted(r['trial'] for r in trials) == [1, 2, 3]
        assert all(r['output_files'] == count + 5 for r in trials)
        assert all(r['package_trees'] == (count if mode == 'existing' else 0) for r in trials)
        assert all(r['dpkg_deb_processes'] == (count if mode == 'existing' else 0) for r in trials)
        modes[mode] = {k: statistics.median(r[k] for r in trials) for k in
                       ('wall_seconds', 'peak_summed_rss_kib', 'wait4_maxrss_kib', 'output_bytes')}
        modes[mode]['packages_per_second'] = count / modes[mode]['wall_seconds']
    summary.append({'packages': count, 'modes': modes,
                    'speedup': modes['existing']['wall_seconds'] / modes['direct']['wall_seconds']})
print(json.dumps(summary, indent=2))
