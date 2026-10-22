"""Isolated CLI integration checks; never contacts the user's systemd instance."""
import importlib.util
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('bukupan', Path(__file__).with_name('bukupan.py'))
cli = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cli)


class Commands(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='bukupan tests ')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.old_env = os.environ.copy()
        self.addCleanup(lambda: (os.environ.clear(), os.environ.update(self.old_env)))
        os.environ['XDG_CONFIG_HOME'] = str(self.base / 'config space %h')
        os.environ['XDG_STATE_HOME'] = str(self.base / 'state space')
        self.bin = self.base / 'bin'
        self.bin.mkdir()
        os.environ['PATH'] = str(self.bin) + ':' + os.environ['PATH']
        os.environ['TEST_LOG'] = str(self.base / 'calls')
        for name, body in {
            'systemctl': 'echo "$*" >> "$TEST_LOG"\n[ "${2:-}" != status ] || exit 3\n[ "${FAIL_RELOAD:-}" != yes ] || [ "${2:-}" != daemon-reload ]',
            'loginctl': '[ "$1" != enable-linger ] || touch "$TEST_LOG.linger"\necho "$*" >> "$TEST_LOG"\nif [ "$1" = show-user ]; then if [ -f "$TEST_LOG.linger" ]; then echo yes; else echo "${TEST_LINGER:-yes}"; fi; fi',
            'nix-store': 'ln -s "$5" "$2"',
        }.items():
            path = self.bin / name
            path.write_text('#!/bin/sh\n' + body + '\n')
            path.chmod(0o755)
        cli.SERVER = str(self.base / 'server')
        cli.CLIENT = str(self.base / 'client')
        cli.TEMPLATE = str(Path(__file__).resolve().parent / 'Caddyfile.template')
        self.target = self.base / 'sites space/site.caddy'

    def command(self, *args):
        sys.argv = ['bukupan', *map(str, args)]
        cli.main()

    def test_sites(self):
        self.command('site', 'install', '--target', self.target, '--domain', 'http://localhost:8765')
        self.assertFalse((self.base / 'calls').read_text() if (self.base / 'calls').exists() else '')
        content = self.target.read_text()
        self.assertIn('/ws', content)
        self.assertIn('/healthz', content)
        self.assertIn('"' + cli.CLIENT + '"', content)
        parent = self.base / 'Caddyfile'
        parent.write_text('{\n admin off\n auto_https off\n}\nimport "' + str(self.target) + '"\n')
        subprocess.run(['caddy', 'adapt', '--config', str(parent), '--adapter', 'caddyfile'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        cli.CLIENT += '-new'
        self.command('site', 'install', '--target', self.target, '--domain', 'example.com')
        roots = list((self.base / 'state space/bukupan/gcroots').glob('site-*/*'))
        self.assertEqual(len(roots), 1)
        self.assertEqual(os.readlink(roots[0]), cli.CLIENT)
        self.command('site', 'uninstall', '--target', self.target)
        self.command('site', 'uninstall', '--target', self.target)
        self.target.write_text('unmanaged')
        with self.assertRaises(ValueError):
            self.command('site', 'install', '--target', self.target, '--domain', 'example.com')
        with self.assertRaises(ValueError):
            self.command('site', 'uninstall', '--target', self.target)
        self.assertEqual(self.target.read_text(), 'unmanaged')

    def test_live_routes(self):
        import http.client
        import socket
        import time

        if not os.environ.get('BUKUPAN_TEST_SERVER'):
            self.skipTest('BUKUPAN_TEST_SERVER is required')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            backend_port = sock.getsockname()[1]
        backend = subprocess.Popen([os.environ['BUKUPAN_TEST_SERVER'], '-listen', f'127.0.0.1:{backend_port}'])
        self.addCleanup(backend.wait, timeout=5)
        self.addCleanup(backend.terminate)
        for path, content in [('index.html', 'app'), ('favicon.ico', 'icon'), ('main.js', 'js')]:
            asset = Path(cli.CLIENT) / path
            asset.parent.mkdir(parents=True, exist_ok=True)
            asset.write_text(content)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        self.command('site', 'install', '--target', self.target, '--domain', f'http://127.0.0.1:{port}', '--listen-port', backend_port)
        parent = self.base / 'Caddyfile'
        parent.write_text('{\n admin off\n auto_https off\n}\nimport "' + str(self.target) + '"\n')
        proc = subprocess.Popen(['caddy', 'run', '--config', str(parent), '--adapter', 'caddyfile'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            for _ in range(100):
                try:
                    with socket.create_connection(('127.0.0.1', port), timeout=0.1):
                        break
                except OSError:
                    time.sleep(0.02)
            for path, expected in [('/index.html', b'app'), ('/favicon.ico', b'icon'), ('/main.js', b'js'), ('/healthz', b'ok\n')]:
                connection = http.client.HTTPConnection('127.0.0.1', port, timeout=3)
                connection.request('GET', path)
                response = connection.getresponse()
                self.assertEqual(response.status, 200)
                self.assertEqual(response.read(), expected)
                connection.close()
            connection = http.client.HTTPConnection('127.0.0.1', port, timeout=3)
            connection.request('GET', '/ws', headers={'Connection': 'Upgrade', 'Upgrade': 'websocket', 'Sec-WebSocket-Version': '13', 'Sec-WebSocket-Key': 'dGhlIHNhbXBsZSBub25jZQ=='})
            self.assertEqual(connection.getresponse().status, 101)
            connection.close()
        finally:
            proc.terminate()
            proc.wait(timeout=5)

    @unittest.skipUnless(sys.platform == 'linux', 'systemd requires Linux')
    def test_services(self):
        args = ('service', 'install')
        self.command(*args)
        with self.assertRaises(ValueError):
            self.command(*args)
        unit = self.base / 'config space %h/systemd/user/bukupan.service'
        self.assertIn('/bin/bukupan-server', unit.read_text())
        for action in ('start', 'start', 'stop', 'stop', 'restart'):
            self.command('service', action)
        with self.assertRaises(SystemExit) as result:
            self.command('service', 'status')
        self.assertEqual(result.exception.code, 3)
        self.command('service', 'uninstall')
        self.command('service', 'uninstall')
        self.assertFalse(unit.exists())
        calls = (self.base / 'calls').read_text()
        self.assertIn('--user disable --now bukupan.service', calls)
        self.assertNotIn('disable-linger', calls)
        os.environ['FAIL_RELOAD'] = 'yes'
        with self.assertRaises(subprocess.CalledProcessError):
            self.command(*args)
        self.assertFalse(unit.exists())
        self.assertFalse((self.base / 'state space/bukupan/gcroots/backend').exists())

    @unittest.skipUnless(sys.platform == 'linux', 'systemd requires Linux')
    def test_linger_and_atomic_failure(self):
        os.environ['TEST_LINGER'] = 'no'
        with self.assertRaises(ValueError):
            self.command('service', 'install')
        self.assertFalse((self.base / 'config space %h').exists())
        self.command('site', 'install', '--target', self.target, '--domain', 'example.com')
        previous = self.target.read_text()
        original = cli.atomic
        cli.atomic = lambda *args, **kwargs: (_ for _ in ()).throw(OSError('write failed'))
        self.addCleanup(setattr, cli, 'atomic', original)
        cli.CLIENT += '-failed'
        with self.assertRaises(OSError):
            self.command('site', 'install', '--target', self.target, '--domain', 'example.com')
        self.assertEqual(self.target.read_text(), previous)
        self.assertEqual(len(list((self.base / 'state space/bukupan/gcroots').glob('site-*/*'))), 1)

    @unittest.skipUnless(sys.platform == 'linux', 'systemd requires Linux')
    def test_service_environment_file_path(self):
        config = self.base / 'config space %h "quoted"'
        os.environ['XDG_CONFIG_HOME'] = str(config)
        # Use a real executable so systemd can verify the generated unit offline.
        server_temp = tempfile.TemporaryDirectory(prefix='bukupan-server-')
        self.addCleanup(server_temp.cleanup)
        cli.SERVER = server_temp.name
        server = Path(cli.SERVER) / 'bin/bukupan-server'
        server.parent.mkdir(parents=True)
        server.symlink_to(sys.executable)
        self.command('service', 'install')
        unit = config / 'systemd/user/bukupan.service'
        self.assertIn('EnvironmentFile=' + str(config / 'bukupan/service.env').replace('%', '%%') + '\n', unit.read_text())
        runtime = self.base / 'runtime'
        runtime.mkdir(mode=0o700)
        result = subprocess.run(['systemd-analyze', '--user', 'verify', str(unit)],
                                env=dict(os.environ, XDG_RUNTIME_DIR=str(runtime)),
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('EnvironmentFile=', result.stderr)

    def test_invalid(self):
        for args in [('site', 'install'), ('serve', '--unknown'), ('service', 'start', '--listen-port', '1'), ('site', 'install', '--target', self.target, '--domain', 'example.com', '--listen-port', '0')]:
            with self.assertRaises(SystemExit):
                self.command(*args)

    @unittest.skipUnless(sys.platform == 'linux', 'systemd requires Linux')
    def test_service_conflicts_and_root_failure(self):
        unit = self.base / 'config space %h/systemd/user/bukupan.service'
        unit.parent.mkdir(parents=True)
        unit.symlink_to(self.base / 'missing')
        with self.assertRaises(ValueError):
            self.command('service', 'install')
        self.assertTrue(unit.is_symlink())
        unit.unlink()
        original = cli.run
        def fail_root(*args, **kwargs):
            result = original(*args, **kwargs)
            if args[0] == 'nix-store':
                raise subprocess.CalledProcessError(1, args)
            return result
        with patch.object(cli, 'run', side_effect=fail_root):
            with self.assertRaises(subprocess.CalledProcessError):
                self.command('service', 'install')
        self.assertFalse(unit.exists())
        self.assertFalse((self.base / 'config space %h/bukupan/service.env').exists())
        root = self.base / 'state space/bukupan/gcroots/backend'
        self.assertFalse(root.is_symlink())

    def test_symlink_protection(self):
        self.target.parent.mkdir(parents=True)
        victim = self.base / 'victim'
        victim.write_text(cli.MARKER + 'keep')
        self.target.symlink_to(victim)
        for action in ('install', 'uninstall'):
            args = ['site', action, '--target', self.target]
            if action == 'install':
                args += ['--domain', 'example.com']
            with self.assertRaises(ValueError):
                self.command(*args)
        self.assertTrue(self.target.is_symlink())
        self.assertEqual(victim.read_text(), cli.MARKER + 'keep')

    def test_platform_and_ipv6(self):
        with patch.object(cli.sys, 'platform', 'darwin'):
            with self.assertRaisesRegex(ValueError, 'Linux'):
                self.command('service', 'status')
            self.command('site', 'install', '--target', self.target,
                         '--domain', 'https://example.com', '--listen-host', '::1')
        self.assertIn('reverse_proxy [::1]:8080', self.target.read_text())
        with patch.object(cli.os, 'execv', side_effect=SystemExit) as execute:
            with self.assertRaises(SystemExit):
                self.command('serve', '--listen-host', '::1', '--listen-port', '65535')
            execute.assert_called_once_with(cli.SERVER + '/bin/bukupan-server',
                                            ['bukupan-server', '-listen', '[::1]:65535'])

    @unittest.skipUnless(sys.platform == 'linux', 'systemd requires Linux')
    def test_explicit_linger(self):
        os.environ['TEST_LINGER'] = 'no'
        self.command('service', 'install', '--enable-linger')
        self.command('service', 'start', '--enable-linger')
        self.command('service', 'restart', '--enable-linger')
        self.command('service', 'uninstall', '--disable-linger')
        calls = (self.base / 'calls').read_text()
        self.assertEqual(calls.count('enable-linger'), 1)
        self.assertIn('disable-linger', calls)

    def test_port_conflict(self):
        import socket
        if not os.environ.get('BUKUPAN_TEST_SERVER'):
            self.skipTest('BUKUPAN_TEST_SERVER is required')
        server = Path(cli.SERVER) / 'bin/bukupan-server'
        server.parent.mkdir(parents=True)
        server.symlink_to(os.environ['BUKUPAN_TEST_SERVER'])
        driver = self.base / 'driver.py'
        driver.write_text(Path(__file__).with_name('bukupan.py').read_text().replace("'@server@'", repr(cli.SERVER)))
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            listener.listen()
            result = subprocess.run([sys.executable, str(driver), 'serve', '--listen-port',
                                     str(listener.getsockname()[1])],
                                    capture_output=True, timeout=5)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'bind', result.stderr)

    def test_serve_exec(self):
        server = Path(cli.SERVER) / 'bin/bukupan-server'
        server.parent.mkdir(parents=True)
        server.write_text('#!/bin/sh\n[ "$2" = 127.0.0.1:4567 ] || exit 1\nexit 37\n')
        server.chmod(0o755)
        driver = self.base / 'driver.py'
        driver.write_text(Path(__file__).with_name('bukupan.py').read_text().replace("'@server@'", repr(cli.SERVER)))
        env = dict(os.environ, BUKUPAN_PORT='4567')
        result = subprocess.run([sys.executable, str(driver), 'serve', '--listen-port', '4567'], env=env)
        self.assertEqual(result.returncode, 37)
        server.write_text('#!/bin/sh\nexec sleep 60\n')
        proc = subprocess.Popen([sys.executable, str(driver), 'serve', '--listen-port', '4567'], env=env)
        try:
            import time
            time.sleep(0.2)
            proc.send_signal(signal.SIGTERM)
            self.assertEqual(proc.wait(timeout=5), -signal.SIGTERM)
        finally:
            if proc.poll() is None:
                proc.kill()


if __name__ == '__main__':
    unittest.main()
