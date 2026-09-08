"""核对本地邮件 TLS 监听实际使用的证书，仅用于开发环境验收。"""

import argparse
import hashlib
from pathlib import Path
import socket
import ssl


def main():
    parser = argparse.ArgumentParser(description="核对邮件 TLS 端口的证书指纹")
    parser.add_argument("certificate", type=Path)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--server-name", required=True)
    parser.add_argument("--ports", type=int, nargs="+", default=[465, 993, 995])
    args = parser.parse_args()
    pem = args.certificate.read_text(encoding="utf-8")
    leaf = pem.split("-----END CERTIFICATE-----", 1)[0] + "-----END CERTIFICATE-----\n"
    expected = hashlib.sha256(ssl.PEM_cert_to_DER_cert(leaf)).hexdigest()
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    # 本地自签名证书用精确指纹验证，不改变应用或系统的信任配置。
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    for port in args.ports:
        with socket.create_connection((args.host, port), timeout=5) as connection:
            with context.wrap_socket(connection, server_hostname=args.server_name) as stream:
                actual = hashlib.sha256(stream.getpeercert(binary_form=True)).hexdigest()
                if actual != expected:
                    raise RuntimeError(f"端口 {port} 未加载预期证书")
                print(f"端口 {port}：TLS 握手成功，证书指纹一致，协议 {stream.version()}")


if __name__ == "__main__":
    main()
