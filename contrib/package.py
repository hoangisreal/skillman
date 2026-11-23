"""Build standalone release archives and their SHA256SUMS."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import zipfile

version, destination = sys.argv[1:]
out = Path(destination)
out.mkdir(parents=True, exist_ok=True)
sums = []
for system in ("linux", "darwin", "windows"):
    for arch in ("amd64", "arm64"):
        with tempfile.TemporaryDirectory() as work:
            windows = system == "windows"
            binary = Path(work) / ("skillman.exe" if windows else "skillman")
            flags = "-s -w -X github.com/hoangisreal/skillman/internal/cli.Version=" + version
            subprocess.run(["go", "build", "-trimpath", "-ldflags", flags, "-o", binary, "./cmd/skillman"],
                           env=dict(os.environ, CGO_ENABLED="0", GOOS=system, GOARCH=arch), check=True)
            archive = out / (f"skillman-{version}-{system}-{arch}" + (".zip" if windows else ".tar.gz"))
            if windows:
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
                    z.write(binary, binary.name)
                    z.write("LICENSE")
            else:
                with tarfile.open(archive, "w:gz") as tar:
                    tar.add(binary, arcname=binary.name)
                    tar.add("LICENSE")
            sums.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n")
(out / "SHA256SUMS").write_text("".join(sums), encoding="ascii")
