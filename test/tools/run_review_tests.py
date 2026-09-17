"""在远程独立目录逐包执行测试，保留全部日志和跳过记录。"""

import argparse
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--only', action='append', default=[])
    parser.add_argument('--run', default='')
    parser.add_argument('--per-test', action='store_true')
    args = parser.parse_args()
    if args.per_test and args.run:
        parser.error('逐用例模式不能同时使用正则筛选')
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
    manifest = json.loads((root / 'manifest.json').read_text(encoding='utf-8'))
    if not manifest:
        parser.error('测试清单为空')
    unknown = set(args.only) - {item['directory'] for item in manifest}
    if unknown:
        parser.error('测试包不存在：' + ', '.join(sorted(unknown)))
    output = root / ('results-' + time.strftime('%Y%m%d-%H%M%S'))
    output.mkdir(mode=0o700)
    results = []
    for item in manifest:
        if args.only and item['directory'] not in args.only:
            continue
        directory = (root / item['directory']).resolve()
        assert directory.is_relative_to(root)
        binary = directory / 'review.test'
        with binary.open('rb') as stream:
            assert stream.read(5) == b'\x7fELF\x02'
        started = time.monotonic()
        log = output / (item['directory'].replace('/', '-') + '.log')
        patterns = [args.run]
        if args.per_test:
            listing = subprocess.check_output([str(binary), '-test.list=.'], cwd=directory,
                                              env=environment, text=True, timeout=30)
            names = [line for line in listing.splitlines()
                     if re.fullmatch(r'(?:Test|Example|Fuzz)\w+', line)]
            if not names:
                raise RuntimeError('未发现可运行用例：' + item['directory'])
            patterns = ['^' + re.escape(name) + '$' for name in names]
        result = dict(item, code=None, log=str(log), status='running', per_test=args.per_test)
        results.append(result)
        summary = output / 'results.json'
        summary.write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
        code = 0
        with log.open('w', encoding='utf-8') as stream:
            for pattern in patterns:
                command = [str(binary), '-test.v', '-test.timeout=180s']
                if pattern:
                    command.append('-test.run=' + pattern)
                process = subprocess.Popen(command, cwd=directory, env=environment,
                                           stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
                try:
                    current = process.wait(timeout=195)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
                    current = 124
                if current != 0:
                    code = current
        lines = log.read_text(encoding='utf-8', errors='replace').splitlines()
        passed = sum(line.startswith('--- PASS:') for line in lines)
        skipped = [line for line in lines if line.lstrip().startswith('--- SKIP:')]
        if not passed and not skipped and code == 0:
            code = 2
        if args.per_test and passed + sum(line.startswith('--- SKIP:') for line in lines) != len(patterns):
            code = code or 2
        result.update(code=code, status='finished', seconds=round(time.monotonic() - started, 2),
                      passed=passed, failed=[line for line in lines if line.lstrip().startswith('--- FAIL:')],
                      skipped=skipped)
        print(json.dumps(result, ensure_ascii=False), flush=True)
        summary.write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
    print('测试记录：' + str(output), flush=True)
    return int(any(item['code'] != 0 for item in results))


if __name__ == '__main__':
    raise SystemExit(main())
