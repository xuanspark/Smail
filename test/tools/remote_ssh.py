import os

import paramiko


def connect_remote(client):
    host = os.environ.get("SMAIL_SSH_HOST", "47.108.78.12")
    user = os.environ.get("SMAIL_SSH_USER", "root")
    password = os.environ.get("SMAIL_SSH_PASSWORD") or None
    key = os.environ.get("SMAIL_SSH_KEY") or None
    client.load_system_host_keys()
    known_hosts = os.environ.get("SMAIL_SSH_KNOWN_HOSTS")
    if known_hosts:
        client.load_host_keys(known_hosts)
    client.set_missing_host_key_policy(paramiko.RejectPolicy())
    client.connect(
        host,
        port=int(os.environ.get("SMAIL_SSH_PORT", "22")),
        username=user,
        password=password,
        key_filename=key,
        look_for_keys=password is None,
        allow_agent=password is None,
        timeout=10,
        auth_timeout=10,
        banner_timeout=10,
    )
    return client
