from pathlib import Path
import stat


def sqlite_inventory(root, expected):
    root = Path(root)
    assert root.is_absolute() and root.resolve(strict=True) == root
    assert (root / 'data').is_dir()
    expected = set(expected)
    assert all(path.is_relative_to(root) for path in expected)
    seen = set()
    pending = [root]
    while pending:
        path = pending.pop()
        assert not path.is_symlink() and not getattr(path, 'is_junction', lambda: False)()
        mode = path.stat().st_mode
        if stat.S_ISDIR(mode):
            pending.extend(path.iterdir())
            continue
        assert stat.S_ISREG(mode)
        name = path.name.lower()
        if name.endswith(('-wal', '-shm', '-journal')):
            assert path.with_name(path.name.rsplit('-', 1)[0]) in expected
            continue
        with path.open('rb') as stream:
            header = stream.read(16)
        if header == b'SQLite format 3\x00' or name.endswith('.db') or '.sqlite' in name:
            assert path in expected
            seen.add(path)
    assert seen == expected
    return sorted(seen)
