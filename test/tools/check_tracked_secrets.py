"""检查已跟踪文件中的云凭据，报告只包含位置，不回显敏感值。"""

import ast
from pathlib import Path
import re
import subprocess
import sys


ACCESS_KEY = re.compile(r'LTAI[A-Za-z0-9]{16,}')
PRIVATE_KEY = re.compile(r'-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE' + r' KEY-----')


def scan_source(name, source):
    findings = set()
    for number, line in enumerate(source.splitlines(), 1):
        if ACCESS_KEY.search(line) or PRIVATE_KEY.search(line):
            findings.add((name, number, 'credential_literal'))
    if name.endswith('.py'):
        try:
            tree = ast.parse(source)
        except SyntaxError:
            findings.add((name, 1, 'python_parse_failed'))
            return sorted(findings)
        for node in ast.walk(tree):
            if not isinstance(node, ast.Assign) or not isinstance(node.value, ast.Constant):
                continue
            if not isinstance(node.value.value, str) or not node.value.value:
                continue
            for target in node.targets:
                if isinstance(target, ast.Name) and target.id.lower() in {'ak', 'sk'}:
                    findings.add((name, node.lineno, 'cloud_key_assignment'))
    return sorted(findings)


def main():
    root = Path(__file__).resolve().parents[2]
    result = subprocess.run(['git', 'ls-files', '-z'], cwd=root, check=True, capture_output=True)
    findings = []
    for name in result.stdout.decode('utf-8').split('\0'):
        if not name:
            continue
        path = root / name
        if not path.is_file():
            continue
        try:
            source = path.read_text(encoding='utf-8-sig')
        except UnicodeDecodeError:
            continue
        findings.extend(scan_source(name, source))
    for name, line, category in findings:
        print(f'{name}:{line}: {category}')
    print('凭据检查：' + ('不通过' if findings else '通过'))
    return 1 if findings else 0


if __name__ == '__main__':
    sys.exit(main())
