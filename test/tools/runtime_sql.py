"""隔离实机验收的 SQL 数据库适配；不删除数据库，不记录连接凭据。"""

import base64
import json
import os
import re
import secrets
import subprocess

from database_inventory import sqlite_inventory


def command(args, sql, env=None):
    result = subprocess.run(args, input=sql, text=True, capture_output=True,
                            timeout=30, env=env, encoding='utf-8')
    if result.returncode:
        # 驱动错误可能包含连接参数，不能直接传播到验收日志。
        raise RuntimeError('隔离测试数据库操作失败，请在受保护环境核查')
    return result.stdout.strip()


class SQLDatabase:
    def __init__(self, settings):
        assert set(settings) == {'kind', 'name', 'user', 'password'}
        assert settings['kind'] in ('mysql', 'postgres')
        assert re.fullmatch(r'smail_runtime_test_[0-9a-f]{12}', settings['name'])
        assert re.fullmatch(r'smail_rt_[0-9a-f]{12}', settings['user'])
        assert re.fullmatch(r'[A-Za-z0-9_-]{32,64}', settings['password'])
        self.settings = settings.copy()
        self.kind = settings['kind']
        self.port = 3306 if self.kind == 'mysql' else 5432

    def setup_payload(self):
        return {'step': 'database', 'action': 'set', 'db_type': self.kind,
                'db_mode': 'fields', 'db_host': '127.0.0.1', 'db_port': str(self.port),
                'db_name': self.settings['name'], 'db_user': self.settings['user'],
                'db_password': self.settings['password']}

    def query(self, sql):
        env = os.environ.copy()
        if self.kind == 'mysql':
            env['MYSQL_PWD'] = self.settings['password']
            args = ['mysql', '--protocol=tcp', '--host=127.0.0.1', '--port=3306',
                    '--user=' + self.settings['user'], '--database=' + self.settings['name'],
                    '--default-character-set=utf8mb4', '--batch', '--raw', '--skip-column-names']
        else:
            env['PGPASSWORD'] = self.settings['password']
            env['PGCLIENTENCODING'] = 'UTF8'
            args = ['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At', '--host=127.0.0.1',
                    '--port=5432', '--username=' + self.settings['user'],
                    '--dbname=' + self.settings['name']]
        return command(args, sql, env)

    def assert_empty_users(self):
        table = '`user`' if self.kind == 'mysql' else '"user"'
        assert self.query('SELECT COUNT(*) FROM ' + table) == '0'

    def verify_message(self, message_id, account, body, attachment):
        assert re.fullmatch(r'[a-zA-Z0-9@._-]+', message_id)
        constructor = 'JSON_OBJECT' if self.kind == 'mysql' else 'json_build_object'
        content = self.query("SELECT " + constructor + "('id',id,'text',text,'attachments',attachments)"
                             " FROM email WHERE msg_id='" + message_id + "'")
        rows = content.splitlines()
        assert len(rows) == 1
        record = json.loads(rows[0])
        assert body in record['text']
        attachments = record['attachments']
        if isinstance(attachments, str):
            attachments = json.loads(attachments)
        assert len(attachments) == 1 and attachments[0]['Filename'] == '验收附件.txt'
        assert base64.b64decode(attachments[0]['Content']) == attachment
        table = '`user`' if self.kind == 'mysql' else '"user"'
        assert isinstance(record['id'], int) and record['id'] > 0
        owners = self.query('SELECT u.account FROM user_email e JOIN ' + table +
                            ' u ON u.id=e.user_id WHERE e.email_id=' + str(record['id']))
        assert owners.splitlines() == [account]

    def integrity(self, root):
        # 配置历史和业务表必须位于所选 SQL 库，运行目录不能悄悄创建 SQLite。
        sqlite_inventory(root, [])
        count = int(self.query('SELECT COUNT(*) FROM config_records'))
        assert count > 0
        return {'type': self.kind, 'database': self.settings['name'],
                'config_records': count, 'sqlite_files': 0}


class SQLFixture(SQLDatabase):
    def __init__(self, kind):
        token = secrets.token_hex(6)
        super().__init__({'kind': kind, 'name': 'smail_runtime_test_' + token,
                          'user': 'smail_rt_' + token, 'password': secrets.token_urlsafe(30)})
        self.account_created = False

    def admin(self, sql, database=None):
        if self.kind == 'mysql':
            args = ['mysql', '--protocol=socket', '--batch', '--skip-column-names']
        else:
            args = ['runuser', '-u', 'postgres', '--', 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At']
            if database:
                assert database == self.settings['name']
                args += ['--dbname=' + database]
        return command(args, sql)

    def create(self):
        name, user, password = (self.settings[key] for key in ('name', 'user', 'password'))
        if self.kind == 'mysql':
            self.admin("CREATE USER '" + user + "'@'localhost' IDENTIFIED BY '" + password +
                       "' WITH MAX_USER_CONNECTIONS 24;")
            self.account_created = True
            self.admin('CREATE DATABASE `' + name + '` CHARACTER SET utf8mb4;')
            # 转义授权模式中的下划线，只授权本轮唯一数据库。
            self.admin('GRANT ALL ON `' + name.replace('_', r'\_') + "`.* TO '" + user + "'@'localhost';")
            count = self.admin("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='" + name + "';")
        else:
            self.admin('CREATE ROLE ' + user + " LOGIN CONNECTION LIMIT 24 PASSWORD '" + password + "';")
            self.account_created = True
            self.admin('CREATE DATABASE ' + name + ' OWNER ' + user + ';')
            self.admin('GRANT ALL ON SCHEMA public TO ' + user + ';', name)
            count = self.admin("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public';", name)
        assert count == '0', '首次安装必须使用空数据库'

    def lock(self):
        if not self.account_created:
            return
        user = self.settings['user']
        if self.kind == 'mysql':
            self.admin("ALTER USER '" + user + "'@'localhost' ACCOUNT LOCK;")
            assert self.admin("SELECT account_locked FROM mysql.user WHERE User='" + user +
                              "' AND Host='localhost';") == 'Y', '测试数据库账号锁定未生效'
        else:
            self.admin('ALTER ROLE ' + user + ' NOLOGIN;')
            assert self.admin("SELECT rolcanlogin FROM pg_roles WHERE rolname='" + user +
                              "';") == 'f', '测试数据库账号锁定未生效'
