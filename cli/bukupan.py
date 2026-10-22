#!/usr/bin/env python3
"""Manage independent bukupan sites and the per-user backend service."""
import argparse
import hashlib
import ipaddress
import os
from pathlib import Path
import re
import string
import subprocess
import sys
import tempfile

SERVER = '@server@'
CLIENT = '@client@'
TEMPLATE = '@template@'
MARKER = '# Managed by bukupan v1\n'


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def absolute(value):
    return Path(os.path.abspath(os.path.expanduser(str(value))))


def exists(path):
    return path.exists() or path.is_symlink()


def owned(path):
    if path.is_symlink() or not path.is_file() or not path.read_text().startswith(MARKER):
        raise ValueError(f'not a bukupan-managed file: {path}')


def atomic(path, text, mode=0o644):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix='.bukupan-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as stream:
            stream.write(text)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(name, mode)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def root(path, package):
    path.parent.mkdir(parents=True, exist_ok=True)
    if exists(path):
        if not path.is_symlink() or os.readlink(path) != package:
            raise ValueError(f'unexpected GC root: {path}')
        return False
    try:
        run('nix-store', '--add-root', str(path), '--indirect', '-r', package, stdout=subprocess.DEVNULL)
    except BaseException:
        if path.is_symlink() and os.readlink(path) == package:
            path.unlink()
        raise
    return True


def remove_root(path):
    if exists(path):
        if not path.is_symlink():
            raise ValueError(f'not a GC root symlink: {path}')
        path.unlink()


def linger(enable=False):
    user = str(os.getuid())
    state = run('loginctl', 'show-user', user, '--property=Linger', '--value', capture_output=True, text=True).stdout.strip()
    if state == 'no' and enable:
        run('loginctl', 'enable-linger', user)
        return linger()
    if state != 'yes':
        raise ValueError('user linger is required; use --enable-linger or loginctl enable-linger')


def ctl(*args):
    return run('systemctl', '--user', *args)


def quoted(value):
    # Environment file values and Caddy strings accept these escapes.
    return '"' + str(value).replace('\\', '\\\\').replace('"', '\\"') + '"'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    groups = parser.add_subparsers(dest='group', required=True)
    site = groups.add_parser('site', help='manage a Caddy site and frontend assets')
    sites = site.add_subparsers(dest='action', required=True)
    service = groups.add_parser('service', help='manage the systemd user backend')
    services = service.add_subparsers(dest='action', required=True)
    listen(groups.add_parser('serve', help='run backend in foreground'))
    for action in ('install', 'uninstall'):
        command = sites.add_parser(action, description={
            'install': 'Write or update a managed site atomically. Import it from your Caddyfile and reload Caddy externally.',
            'uninstall': 'Remove the managed site and its asset GC roots. Reload Caddy externally.',
        }[action])
        command.add_argument('--target', required=True, type=absolute, help='Caddy site file path (required)')
        if action == 'install':
            command.add_argument('--domain', required=True)
            listen(command)
    for action in ('install', 'start', 'stop', 'restart', 'status', 'uninstall'):
        command = services.add_parser(action, description={
            'install': 'Install a stopped, disabled user service. Requires user linger.',
            'start': 'Enable automatic startup and start the service. Requires linger.',
            'stop': 'Disable automatic startup and stop the service.',
            'restart': 'Restart or start the service, preserving automatic startup. Requires linger.',
            'status': 'Pass through systemd status output and exit code.',
            'uninstall': 'Remove the service and backend GC root, preserving sites.',
        }[action])
        if action in ('install', 'start', 'restart'):
            command.add_argument('--enable-linger', action='store_true', help='enable user linger if needed')
        if action == 'uninstall':
            command.add_argument('--disable-linger', action='store_true', help='also disable user linger')
        if action == 'install':
            listen(command)
    args = parser.parse_args()
    if args.group == 'serve':
        os.execv(SERVER + '/bin/bukupan-server', ['bukupan-server', '-listen', address(args)])
    if args.group == 'service' and sys.platform != 'linux':
        raise ValueError('service commands require Linux systemd')
    config = absolute(os.environ.get('XDG_CONFIG_HOME', str(Path.home() / '.config')))
    state = absolute(os.environ.get('XDG_STATE_HOME', str(Path.home() / '.local/state')))
    roots = state / 'bukupan/gcroots'
    if args.group == 'site':
        target = args.target
        site_roots = roots / ('site-' + hashlib.sha256(os.fsencode(target)).hexdigest())
        gcroot = site_roots / hashlib.sha256(CLIENT.encode()).hexdigest()
        if exists(target):
            owned(target)
        if args.action == 'uninstall':
            target.unlink(missing_ok=True)
            if site_roots.exists():
                for entry in site_roots.iterdir():
                    remove_root(entry)
                site_roots.rmdir()
            print('Site removed. Update imports, then validate and apply your master Caddy configuration.')
            return
        if not re.fullmatch(r'(?:https?://)?(?:[A-Za-z0-9][A-Za-z0-9.-]*|\[[0-9A-Fa-f:]+\])(?::[0-9]+)?', args.domain):
            raise ValueError('invalid --domain site address')
        # Store paths are immutable; the target-specific root keeps these assets alive.
        upstream = address(args)
        content = string.Template(Path(TEMPLATE).read_text()).substitute(domain=args.domain, client_root=quoted(CLIENT), upstream=upstream)
        created = root(gcroot, CLIENT)
        try:
            atomic(target, MARKER + content)
        except BaseException:
            if created:
                remove_root(gcroot)
            raise
        for entry in site_roots.iterdir():
            if entry != gcroot:
                remove_root(entry)
        print(f'Site installed: {target}. Import this file in your master Caddyfile, then validate and apply Caddy configuration.')
        return
    unit = config / 'systemd/user/bukupan.service'
    env = config / 'bukupan/service.env'
    gcroot = roots / 'backend'
    if args.action == 'install':
        for path in (unit, env, gcroot):
            if exists(path):
                raise ValueError(f'already installed or conflicting path: {path}')
        if any(c in str(env) for c in '\n\r\0'):
            raise ValueError('paths must not contain control characters')
        linger(args.enable_linger)
        ctl('show-environment')
        root(gcroot, SERVER)
        try:
            atomic(env, MARKER + 'BUKUPAN_LISTEN=' + quoted(address(args)) + '\n', 0o600)
            # EnvironmentFile takes one literal path, including spaces; quotes
            # become part of the path. Only systemd specifiers need escaping.
            atomic(unit, MARKER + '[Unit]\nDescription=Bukupan\nAfter=network.target\n\n[Service]\nEnvironmentFile=' + str(env).replace('%', '%%') + '\nExecStart=' + SERVER + '/bin/bukupan-server -listen ${BUKUPAN_LISTEN}\nRestart=on-failure\nRestartSec=2s\n\n[Install]\nWantedBy=default.target\n')
            ctl('daemon-reload')
            ctl('disable', '--now', 'bukupan.service')
        except BaseException:
            unit.unlink(missing_ok=True)
            env.unlink(missing_ok=True)
            remove_root(gcroot)
            subprocess.run(['systemctl', '--user', 'daemon-reload'], check=False)
            raise
        return
    if args.action == 'status':
        sys.exit(subprocess.run(['systemctl', '--user', 'status', 'bukupan.service']).returncode)
    if args.action == 'uninstall' and not exists(unit) and not exists(env) and not exists(gcroot):
        if args.disable_linger:
            run('loginctl', 'disable-linger', str(os.getuid()))
        return
    owned(unit)
    owned(env)
    if args.action in ('start', 'restart'):
        linger(args.enable_linger)
    if args.action == 'start':
        ctl('enable', '--now', 'bukupan.service')
    elif args.action == 'restart':
        ctl('restart', 'bukupan.service')
    elif args.action in ('stop', 'uninstall'):
        ctl('disable', '--now', 'bukupan.service')
        if args.action == 'uninstall':
            unit.unlink()
            env.unlink()
            remove_root(gcroot)
            ctl('daemon-reload')
            if args.disable_linger:
                run('loginctl', 'disable-linger', str(os.getuid()))


def address(args):
    host = args.listen_host
    return f'[{host}]:{args.listen_port}' if ':' in host else f'{host}:{args.listen_port}'

def listen(parser):
    parser.add_argument('--listen-host', default='127.0.0.1', type=host, help='backend host (default: 127.0.0.1)')
    parser.add_argument('--listen-port', default=8080, type=port, help='backend port (default: 8080)')


def host(value):
    if ':' in value:
        try:
            ipaddress.IPv6Address(value)
        except ValueError as error:
            raise argparse.ArgumentTypeError('invalid IPv6 host') from error
    elif not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]*', value):
        raise argparse.ArgumentTypeError('invalid listen host')
    return value


def port(value):
    try:
        number = int(value)
        if 1 <= number <= 65535:
            return number
    except ValueError:
        pass
    raise argparse.ArgumentTypeError('port must be between 1 and 65535')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f'bukupan: {error}', file=sys.stderr)
        sys.exit(1)
