import argparse
import pathlib
import shlex
import sys

import paramiko

from remote_ssh import connect_remote


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--command")
    parser.add_argument("--upload", nargs=2, action="append", default=[])
    args = parser.parse_args()
    client = connect_remote(paramiko.SSHClient())
    try:
        with client.open_sftp() as sftp:
            for source, destination in args.upload:
                target = pathlib.PurePosixPath(destination)
                if (not target.is_absolute() or ".." in target.parts
                        or len(target.parts) < 5 or target.parts[1:3] != ("var", "tmp")
                        or not target.parts[3].startswith("smail-review-")):
                    raise ValueError("上传路径必须位于项目审查临时目录")
                command = "install -d -m 0755 " + shlex.quote(str(target.parent))
                _, output, error = client.exec_command(command)
                if output.channel.recv_exit_status() != 0:
                    raise RuntimeError(error.read().decode("utf-8", "replace"))
                sftp.put(source, destination)
                sftp.chmod(destination, 0o755 if destination.endswith(".test") else 0o644)
        if args.command:
            _, output, error = client.exec_command(args.command, timeout=900)
            for line in output:
                print(line, end="", flush=True)
            errors = error.read().decode("utf-8", "replace")
            if errors:
                print(errors, file=sys.stderr)
            return output.channel.recv_exit_status()
        return 0
    finally:
        client.close()


if __name__ == "__main__":
    raise SystemExit(main())
