"""只向隔离网络提供单个本机数据库端口，不向宿主网络新增监听端口。"""

from contextlib import ExitStack
import os
from pathlib import Path
import select
import socket
import socketserver
import threading


class DatabaseBridge:
    def __init__(self, pid, port):
        assert isinstance(pid, int) and pid > 1
        assert port in (3306, 5432)
        namespace = Path('/proc') / str(pid) / 'ns/net'
        assert namespace.readlink() != Path('/proc/self/ns/net').readlink()
        limit = threading.BoundedSemaphore(24)

        class Handler(socketserver.BaseRequestHandler):
            def handle(self):
                if not limit.acquire(blocking=False):
                    return
                try:
                    with socket.create_connection(('127.0.0.1', port), timeout=15) as upstream:
                        streams = (self.request, upstream)
                        for stream in streams:
                            stream.settimeout(30)
                        while True:
                            ready, _, _ = select.select(streams, [], [], 30)
                            if not ready:
                                return
                            for source in ready:
                                data = source.recv(65536)
                                if not data:
                                    return
                                target = upstream if source is self.request else self.request
                                target.sendall(data)
                except (OSError, TimeoutError):
                    return
                finally:
                    limit.release()

        class Server(socketserver.ThreadingTCPServer):
            daemon_threads = True
            allow_reuse_address = False

        with ExitStack() as stack:
            host = stack.enter_context(open('/proc/self/ns/net', 'rb'))
            isolated = stack.enter_context(open(namespace, 'rb'))
            try:
                os.setns(isolated.fileno(), 0)
                self.server = Server(('127.0.0.1', port), Handler)
            finally:
                os.setns(host.fileno(), 0)
        # 监听套接字属于隔离网络，后续工作线程在恢复后的宿主网络连接数据库。
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)
        assert not self.thread.is_alive(), '数据库验收桥接未停止'
