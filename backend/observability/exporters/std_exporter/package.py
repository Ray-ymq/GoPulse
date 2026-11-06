"""构建包含清单、配置声明和二进制的 GoPulse 插件包。"""

import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile


def package(root, arch, version):
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        raise ValueError("版本必须由三个非负整数组成")
    descriptor_data = (root / "descriptor.json").read_bytes()
    descriptor = json.loads(descriptor_data)
    source = descriptor["source"]
    if not re.fullmatch(r"[a-z][a-z0-9_]*", source):
        raise ValueError("数据源名称无效")
    entrypoint = descriptor["entrypoint"]
    if entrypoint != "bin/gopulse-" + source + "-exporter":
        raise ValueError("二进制路径与数据源名称不一致")
    if descriptor["runtime_contract_version"] != 1 or descriptor["metrics_contract_version"] != 2:
        raise ValueError("插件协议版本无效")
    if descriptor["config_schema_path"] != "config.schema.json":
        raise ValueError("配置声明路径无效")
    schema_data = (root / "config.schema.json").read_bytes()
    schema = json.loads(schema_data)
    if schema["schema_version"] != 1 or schema["plugin_id"] != descriptor["id"]:
        raise ValueError("配置声明与插件标识不一致")
    build = root / "build"
    build.mkdir(exist_ok=True)
    output = build / ("gopulse-" + source + "-" + version + "-linux-" + arch + ".tar.gz")
    with tempfile.TemporaryDirectory(dir=build) as temporary:
        binary = Path(temporary) / "exporter"
        go = os.environ.get("GO", "go")
        module = subprocess.check_output([go, "list", "-f", "{{.Module.Path}}", "."], cwd=root, text=True).strip()
        env = dict(os.environ, GOOS="linux", GOARCH=arch, CGO_ENABLED="0")
        subprocess.run(
            [go, "build", "-trimpath", "-buildvcs=false",
             "-ldflags=-buildid= -X " + module + "/common.Version=" + version,
             "-o", str(binary), "."], cwd=root, env=env, check=True,
        )
        binary_data = binary.read_bytes()
        manifest = {key: descriptor[key] for key in (
            "id", "name", "source", "entrypoint", "health_path", "metrics_path",
            "runtime_contract_version", "metrics_contract_version", "config_schema_path",
        )}
        manifest.update(
            schema_version=2, version=version, kind="metrics-exporter", os="linux", arch=arch,
            entrypoint_sha256=hashlib.sha256(binary_data).hexdigest(),
            config_schema_sha256=hashlib.sha256(schema_data).hexdigest(),
        )
        members = [
            ("plugin.json", json.dumps(manifest, separators=(",", ":")).encode() + b"\n", 0o644),
            ("descriptor.json", descriptor_data, 0o644),
            ("config.schema.json", schema_data, 0o644),
            (entrypoint, binary_data, 0o755),
        ]
        staged = Path(temporary) / "package.tar.gz"
        with staged.open("wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                for name, data, mode in members:
                    info = tarfile.TarInfo(name)
                    info.size, info.mode = len(data), mode
                    archive.addfile(info, io.BytesIO(data))
        staged.replace(output)
    return output


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--arch", choices=("amd64", "arm64"), default="amd64")
    parser.add_argument("--version")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    print(package(root, args.arch, args.version or (root / "VERSION").read_text().strip()))
