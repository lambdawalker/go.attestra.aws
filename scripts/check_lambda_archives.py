"""Validate deployment packages using the application's Go build manifest."""

import json
from pathlib import Path
import shutil
from struct import unpack_from
import subprocess
from zipfile import ZipFile


def load_lambda_names(root):
    go = shutil.which("go")
    if go is None:
        raise RuntimeError("Install Go 1.27.3+ and add it to PATH.")
    result = subprocess.run(
        [go, "-C", str(root / "tools" / "deploy"), "run", "./cmd/archive-manifest"],
        check=True, capture_output=True, text=True,
    )
    names = json.loads(result.stdout)
    if not isinstance(names, list) or not names or any(not isinstance(name, str) or not name for name in names):
        raise ValueError("Invalid Lambda archive manifest from Go builder")
    return names


def validate_archive(archive):
    with ZipFile(archive) as package:
        entries = package.infolist()
        if len(entries) != 1 or entries[0].filename != "bootstrap":
            raise ValueError(f"{archive}: package must contain only bootstrap")
        bootstrap = entries[0]
        if not (bootstrap.external_attr >> 16 & 0o111):
            raise ValueError(f"{archive}: bootstrap is not executable")
        with package.open(bootstrap) as stream:
            header = stream.read(20)
        if len(header) < 20 or header[:4] != b"\x7fELF" or unpack_from("<H", header, 18)[0] != 183:
            raise ValueError(f"{archive}: bootstrap must be an ARM64 Linux ELF executable")


def validate_archives(root):
    for name in load_lambda_names(root):
        archive = root / "dist" / f"{name}.zip"
        validate_archive(archive)
        print(f"Valid Lambda archive: {archive}")


if __name__ == "__main__":
    validate_archives(Path(__file__).resolve().parents[1])
