"""只读核对 Linux 主机上的 Smail 资源隔离，不执行部署、重启或压测。"""

import argparse
from datetime import datetime, timezone
from decimal import Decimal
import json
import os
from pathlib import Path
import re
import subprocess


MIB = 1024 * 1024
PROPERTIES = (
    'ActiveState', 'SubState', 'MainPID', 'InvocationID', 'ControlGroup', 'NRestarts',
    'MemoryHigh', 'MemoryMax', 'MemorySwapMax', 'TasksMax', 'OOMPolicy', 'KillMode',
    'Restart', 'RestartUSec', 'StartLimitIntervalUSec', 'StartLimitBurst', 'TimeoutStopUSec',
)


def systemd_state():
    result = subprocess.run(
        ['systemctl', 'show', 'smail.service', '--property=' + ','.join(PROPERTIES)],
        check=True, capture_output=True, text=True, timeout=10,
        env={**os.environ, 'LC_ALL': 'C'},
    )
    state = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
    if any(name not in state for name in PROPERTIES):
        raise ValueError('服务管理器未提供完整资源属性')
    return {name: state[name] for name in PROPERTIES}


def limit(value):
    if value in ('max', 'infinity'):
        return None
    number = int(value)
    if number < 0:
        raise ValueError('资源限额不能是负数')
    return number


def go_memory_limit(environment):
    values = [item[len(b'GOMEMLIMIT='):] for item in environment.split(b'\0')
              if item.startswith(b'GOMEMLIMIT=')]
    if len(values) != 1:
        return None
    # 只允许合法的 Go 字节单位，不把其他环境变量或无效值写入报告。
    match = re.fullmatch(rb'([0-9]+)(B|KiB|MiB|GiB|TiB)?', values[0])
    if not match:
        return None
    units = {None: 1, b'B': 1, b'KiB': 1024, b'MiB': MIB,
             b'GiB': 1024 ** 3, b'TiB': 1024 ** 4}
    return int(match[1]) * units[match[2]]


def duration_usec(value):
    if not re.fullmatch(r'(?:[0-9]+(?:\.[0-9]+)?(?:us|ms|s|min|h|d)\s*)+', value):
        return None
    units = {'us': 1, 'ms': 1000, 's': 1000000, 'min': 60000000,
             'h': 3600000000, 'd': 86400000000}
    return int(sum(Decimal(number) * units[unit] for number, unit in
                   re.findall(r'([0-9]+(?:\.[0-9]+)?)(us|ms|s|min|h|d)', value)))


def cpu_quota(value):
    quota, period = value.split()
    amount, interval = limit(quota), int(period)
    if interval <= 0 or (amount is not None and amount <= 0):
        raise ValueError('CPU 限额无效')
    return None if amount is None else amount / interval


def collect():
    before = systemd_state()
    pid = int(before['MainPID'])
    if before['ActiveState'] != 'active' or before['SubState'] != 'running' or pid <= 0:
        raise ValueError('Smail 主服务未处于运行状态')
    proc = Path('/proc') / str(pid)
    base = Path('/sys/fs/cgroup').resolve(strict=True)
    group_name = before['ControlGroup']
    if not group_name.startswith('/') or '..' in Path(group_name).parts:
        raise ValueError('服务控制组路径无效')
    group = (base / group_name.lstrip('/')).resolve(strict=True)
    if group == base or base not in group.parents:
        raise ValueError('Smail 必须使用独立控制组')
    if ('0::' + group_name) not in (proc / 'cgroup').read_text().splitlines():
        raise ValueError('主进程没有加入预期的统一控制组')
    files = ('memory.high', 'memory.max', 'memory.current', 'memory.swap.max',
             'pids.max', 'pids.current', 'cpu.max')
    kernel = {name: (group / name).read_text().strip() for name in files}
    events = dict(line.split() for line in (group / 'memory.events').read_text().splitlines())
    parents = []
    parent = group.parent
    while True:
        limits = {name: (parent / name).read_text().strip() for name in files
                  if (parent / name).exists()}
        parents.append({'path': str(parent.relative_to(base)), 'limits': limits})
        if parent == base:
            break
        parent = parent.parent
    memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
    snapshot = {
        'time_utc': datetime.now(timezone.utc).isoformat(),
        'service': before, 'kernel': kernel, 'parents': parents,
        'oom': {name: int(events[name]) for name in ('oom', 'oom_kill')},
        'go_memory_bytes': go_memory_limit((proc / 'environ').read_bytes()),
        'host_available_bytes': int(memory['MemAvailable'].split()[0]) * 1024,
        'host_total_bytes': int(memory['MemTotal'].split()[0]) * 1024,
    }
    # 读取期间若发生重启或资源属性变更，旧、新进程的数据不能拼成通过证据。
    snapshot['stable'] = before == systemd_state()
    return snapshot


def evaluate(snapshot, reserve_bytes):
    failures = []

    def require(condition, message):
        if not condition:
            failures.append(message)

    service, kernel = snapshot['service'], snapshot['kernel']
    high, maximum = limit(kernel['memory.high']), limit(kernel['memory.max'])
    current, tasks = int(kernel['memory.current']), int(kernel['pids.current'])
    task_maximum = limit(kernel['pids.max'])
    go_limit = snapshot['go_memory_bytes']
    require(reserve_bytes >= 128 * MIB, '必须明确预留至少 128 MiB 的主机及同机服务增长余量')
    require(snapshot['stable'], '取证期间服务重启或资源配置发生变化，需要重新检查')
    require(high is not None and maximum is not None and 0 < high < maximum,
            '必须设置有限且递增的内存高水位与硬上限')
    require(limit(kernel['memory.swap.max']) == 0, '交换空间未限制为零，不符合当前部署预算')
    require(go_limit is not None and high is not None and 64 * MIB <= go_limit < high,
            '主进程缺少有效 GOMEMLIMIT，或没有为非 Go 内存预留高水位余量')
    require(high is not None and current < high, '当前内存已达到高水位或高水位未配置')
    require(task_maximum is not None and 0 < tasks < task_maximum, '任务数上限无效或已经耗尽')
    require(cpu_quota(kernel['cpu.max']) is not None, 'CPU 没有有限配额')
    for property_name, file_name in (('MemoryHigh', 'memory.high'), ('MemoryMax', 'memory.max'),
                                     ('MemorySwapMax', 'memory.swap.max'), ('TasksMax', 'pids.max')):
        require(limit(service[property_name]) == limit(kernel[file_name]),
                property_name + ' 在服务管理器与实际控制组中不一致')
    require(service['OOMPolicy'] in ('stop', 'kill'), 'OOM 后未设置整组停止策略')
    require(service['KillMode'] == 'control-group', '停止服务时未覆盖整个进程组')
    require((duration_usec(service['TimeoutStopUSec']) or 0) >= 90 * 1000000,
            '停止等待不足 90 秒或未设置有效期限，无法覆盖队列及数据库收尾预算')
    require(service['Restart'] == 'on-failure', '服务重启策略不符合故障恢复要求')
    require((duration_usec(service['RestartUSec']) or 0) >= 5 * 1000000, '重启退避时间不足')
    require((duration_usec(service['StartLimitIntervalUSec']) or 0) >= 60 * 1000000
            and 1 <= int(service['StartLimitBurst']) <= 5, '缺少有效的启动频率限制')
    require(all(snapshot['oom'][name] == 0 for name in ('oom', 'oom_kill')),
            '当前服务控制组记录过 OOM，需要核查原因并重新验收')
    growth = None if maximum is None else max(0, maximum - current)
    if growth is not None:
        require(maximum < snapshot['host_total_bytes'], '单服务硬上限已达到主机总内存')
        require(snapshot['host_available_bytes'] - growth >= reserve_bytes,
                '服务增长到硬上限后，主机剩余预算不足')
        for parent in snapshot['parents']:
            values = parent['limits']
            for name in ('memory.high', 'memory.max'):
                bound = limit(values.get(name, 'max'))
                if bound is not None:
                    require(bound - int(values['memory.current']) - growth >= reserve_bytes,
                            '上级控制组 ' + parent['path'] + ' 的 ' + name + ' 余量不足')
            bound = limit(values.get('pids.max', 'max'))
            if bound is not None and task_maximum is not None:
                require(bound - int(values['pids.current']) >= task_maximum - tasks,
                        '上级控制组 ' + parent['path'] + ' 的任务数余量不足')
            parent_cpu = cpu_quota(values.get('cpu.max', 'max 100000'))
            own_cpu = cpu_quota(kernel['cpu.max'])
            if parent_cpu is not None and own_cpu is not None:
                require(own_cpu <= parent_cpu, '上级控制组 ' + parent['path'] + ' 的 CPU 配额更小')
    return {
        'resource_checks_passed': not failures,
        'production_approved': False,
        'reserve_bytes': reserve_bytes,
        'remaining_growth_bytes': growth,
        'failures': failures,
        'scope': '仅检查单次资源快照，不替代容量、持续监控及完整生产验收',
        'snapshot': snapshot,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--reserve-mib', required=True, type=int,
                        help='按同机服务增长与系统开销核算的保留内存，不少于 128 MiB')
    args = parser.parse_args()
    if args.reserve_mib < 128:
        parser.error('保留内存必须不少于 128 MiB，不能用零预算规避检查')
    try:
        if os.name != 'posix' or os.geteuid() != 0:
            raise ValueError('请在 Linux 部署主机使用 root 执行只读检查')
        report = evaluate(collect(), args.reserve_mib * MIB)
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        # 系统命令的错误输出可能携带部署信息，不原样打印环境或异常内容。
        report = {'resource_checks_passed': False, 'production_approved': False,
                  'failures': ['无法获得完整资源证据，请检查服务、root 权限及 cgroup v2 支持']}
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report['resource_checks_passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
