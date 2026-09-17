"""在远程独立目录逐包执行测试，保留全部日志和跳过记录。"""

import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--only', action='append', default=[])
    parser.add_argument('--run', default='')
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    assert root.parent == Path('/var/tmp') and root.name.startswith('smail-review-')
    assert os.geteuid() != 0, '禁止使用 root 执行测试'
    environment = os.environ.copy()
    for name in list(environment):
        if name.startswith('SMAIL_'):
            environment.pop(name)
    environment['GOMAXPROCS'] = '1'
    environment['GOMEMLIMIT'] = '64MiB'
    environment['GOGC'] = '20'
    output = root / ('results-' + time.strftime('%Y%m%d-%H%M%S'))
    output.mkdir(mode=0o700)
    results = []
    for item in json.loads((root / 'manifest.json').read_text(encoding='utf-8')):
        if args.only and item['directory'] not in args.only:
            continue
        directory = (root / item['directory']).resolve()
        assert directory.is_relative_to(root)
        binary = directory / 'review.test'
        with binary.open('rb') as stream:
            assert stream.read(5) == b'\x7fELF\x02'
        started = time.monotonic()
        log = output / (item['directory'].replace('/', '-') + '.log')
        with log.open('w', encoding='utf-8') as stream:
            command = [str(binary), '-test.v', '-test.timeout=180s']
            if args.run:
                command.append('-test.run=' + args.run)
            process = subprocess.Popen(command, cwd=directory, env=environment,
                                       stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = process.wait(timeout=195)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                code = 124
        lines = log.read_text(encoding='utf-8', errors='replace').splitlines()
        result = dict(item, code=code, log=str(log), seconds=round(time.monotonic() - started, 2),
                      passed=sum(line.startswith('--- PASS:') for line in lines),
                      failed=[line for line in lines if line.lstrip().startswith('--- FAIL:')],
                      skipped=[line for line in lines if line.lstrip().startswith('--- SKIP:')])
        results.append(result)
        print(json.dumps(result, ensure_ascii=False), flush=True)
        (output / 'results.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
    print('测试记录：' + str(output), flush=True)
    return int(any(item['code'] != 0 for item in results))


if __name__ == '__main__':
    raise SystemExit(main())
