function Enter-SmailWorkspaceLock {
    param([Parameter(Mandatory = $true)][string]$Root)

    # 构建会归档 Go 嵌入资源，必须与同一源码目录的后端测试互斥。
    # 使用系统互斥量，进程异常退出后也不会留下需要手工删除的锁文件。
    $resolved = (Resolve-Path -LiteralPath $Root -ErrorAction Stop).ProviderPath
    $identity = [IO.Path]::GetFullPath($resolved).TrimEnd('\').ToUpperInvariant()
    $hash = [Security.Cryptography.SHA256]::Create()
    try {
        $digest = [BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($identity))).Replace('-', '')
    } finally {
        $hash.Dispose()
    }
    $mutex = [Threading.Mutex]::new($false, "Local\SmailWorkspace-$digest")
    try {
        $acquired = $false
        try {
            $acquired = $mutex.WaitOne(0)
        } catch [Threading.AbandonedMutexException] {
            # 上一个持有者异常退出时，系统已把互斥量交给当前线程。
            $acquired = $true
        }
        if (-not $acquired) {
            throw '同一工作区正在构建或执行后端测试，请等待结束后重试，避免嵌入资源被并发移动。'
        }
        return $mutex
    } catch {
        $mutex.Dispose()
        throw
    }
}
