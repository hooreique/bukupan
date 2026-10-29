const { test: base, expect } = require('@playwright/test');
const { spawn } = require('node:child_process');
const { once } = require('node:events');
const fs = require('node:fs/promises');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');

const root = path.resolve(__dirname, '../..');

async function freePort() {
  const socket = net.createServer();
  socket.listen(0, '127.0.0.1');
  await once(socket, 'listening');
  const port = socket.address().port;
  await new Promise((resolve, reject) => socket.close(error => error ? reject(error) : resolve()));
  return port;
}

async function stop(child) {
  if (child.pid === undefined || child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, 'exit');
  child.kill('SIGTERM');
  const deadline = setTimeout(() => child.kill('SIGKILL'), 3_000);
  try {
    await exited;
  } finally {
    clearTimeout(deadline);
  }
}

const test = base.extend({
  otherPage: async ({ browser, viewport, deviceScaleFactor }, use) => {
    const context = await browser.newContext({ viewport, deviceScaleFactor });
    try {
      await use(await context.newPage());
    } finally {
      // Playwright captures traces/screenshots for this context too.
      await context.close();
    }
  },

  // Every test owns a fresh single-room server; no shared development instance.
  appURL: async ({}, use, testInfo) => {
    if (!process.env.BUKUPAN_TEST_SERVER) {
      throw new Error('Run ./scripts/test browser to build the test backend first');
    }
    const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'bukupan-browser-'));
    const children = [];
    const logs = [];
    const start = (name, executable, args) => {
      const child = spawn(executable, args, {
        cwd: root,
        env: {
          ...process.env,
          XDG_CONFIG_HOME: path.join(temporary, 'config'),
          XDG_DATA_HOME: path.join(temporary, 'data'),
          XDG_STATE_HOME: path.join(temporary, 'state'),
        },
        stdio: ['ignore', 'pipe', 'pipe'],
      });
      children.push(child);
      child.on('error', error => logs.push(`${name}: ${error.stack}\n`));
      child.stdout.on('data', data => logs.push(`${name}: ${data}`));
      child.stderr.on('data', data => logs.push(`${name}: ${data}`));
      return child;
    };
    const ready = async url => {
      await expect.poll(async () => {
        for (const child of children) {
          if (child.exitCode !== null || child.signalCode !== null || child.pid === undefined) {
            throw new Error(`Test server exited before readiness:\n${logs.join('')}`);
          }
        }
        try {
          const response = await fetch(url, { signal: AbortSignal.timeout(500) });
          return response.ok && (await response.text()) === 'ok\n';
        } catch {
          return false;
        }
      }, { message: `Server readiness: ${url}`, timeout: 10_000 }).toBe(true);
    };
    try {
      const backendPort = await freePort();
      start('backend', process.env.BUKUPAN_TEST_SERVER, ['-listen', `127.0.0.1:${backendPort}`]);
      await ready(`http://127.0.0.1:${backendPort}/healthz`);
      const port = await freePort();
      const config = path.join(temporary, 'Caddyfile');
      await fs.writeFile(config, `{
  admin off
  auto_https off
}
http://127.0.0.1:${port} {
  bind 127.0.0.1
  handle /ws {
    reverse_proxy 127.0.0.1:${backendPort}
  }
  handle /healthz {
    reverse_proxy 127.0.0.1:${backendPort}
  }
  handle_path /__tests/* {
    root * ${JSON.stringify(root)}
    file_server
  }
  handle {
    root * ${JSON.stringify(path.join(root, 'client'))}
    file_server
  }
}
`);
      start('caddy', 'caddy', ['run', '--config', config, '--adapter', 'caddyfile']);
      const url = `http://127.0.0.1:${port}`;
      await ready(`${url}/healthz`);
      await use(url);
    } finally {
      await Promise.all(children.map(stop));
      await fs.rm(temporary, { recursive: true, force: true });
      if (testInfo.status !== testInfo.expectedStatus) {
        await testInfo.attach('servers.log', {
          body: logs.join(''), contentType: 'text/plain',
        });
      }
    }
  },

  // Covers all contexts, including the second participant in a sharing test.
  browserErrors: [async ({ browser }, use, testInfo) => {
    const errors = [];
    const consoleLog = [];
    const watchContext = context => context.on('page', page => {
      page.on('pageerror', error => errors.push(error.stack || String(error)));
      page.on('crash', () => errors.push(`Page crashed: ${page.url()}`));
      page.on('console', message => consoleLog.push(`${message.type()}: ${message.text()}`));
    });
    browser.on('context', watchContext);
    try {
      await use(errors);
    } finally {
      browser.off('context', watchContext);
      if (testInfo.status !== testInfo.expectedStatus || errors.length) {
        await testInfo.attach('browser.log', {
          body: [...errors, ...consoleLog].join('\n'), contentType: 'text/plain',
        });
      }
    }
  }, { auto: true }],
});

// Assert before fixture teardown, so server logs are retained for this failure.
test.afterEach(async ({ browserErrors }) => {
  expect(browserErrors, 'Uncaught browser errors').toEqual([]);
});

module.exports = { test, expect };
