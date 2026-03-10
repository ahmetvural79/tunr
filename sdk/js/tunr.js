/**
 * @tunr/sdk — JavaScript/TypeScript SDK
 * 
 * Use the tunr CLI programmatically from Node.js applications.
 * 
 * @example
 * import { TunrClient } from '@tunr/sdk';
 * 
 * const tunr = new TunrClient();
 * const tunnel = await tunr.share(3000);
 * console.log(tunnel.publicUrl); // https://abc123.tunr.sh
 * 
 * // For Express/Next.js test servers:
 * const url = await tunr.shareServer(app, { port: 3000 });
 */

'use strict';

const { spawn, exec } = require('child_process');
const http = require('http');
const https = require('https');

class TunrClient {
    /**
     * @param {Object} options
     * @param {string} [options.token] - Auth token (optional)
     * @param {string} [options.binary='tunr'] - Path to the tunr binary
     * @param {number} [options.dashPort=19842] - Inspector dashboard port
     */
    constructor(options = {}) {
        this.token = options.token;
        this.binary = options.binary || 'tunr';
        this.dashPort = options.dashPort || 19842;
        this._activeTunnels = [];
    }

    /**
     * Share a local port as a public URL
     * @param {number} port - Local port (e.g. 3000)
     * @param {Object} [opts] - Options
     * @param {string} [opts.subdomain] - Preferred subdomain
     * @param {number} [opts.timeout=15000] - Tunnel startup timeout (ms)
     * @returns {Promise<Tunnel>}
     */
    async share(port, opts = {}) {
        if (port < 1024 || port > 65535) {
            throw new Error(`Invalid port: ${port} (must be between 1024-65535)`);
        }

        const args = ['share', '--port', String(port), '--no-open'];
        if (opts.subdomain) args.push('--subdomain', opts.subdomain);

        return new Promise((resolve, reject) => {
            const proc = spawn(this.binary, args, {
                stdio: ['ignore', 'pipe', 'pipe'],
            });

            let resolved = false;
            const timeout = setTimeout(() => {
                if (!resolved) {
                    proc.kill();
                    reject(new Error('Tunnel failed to start within 15 seconds. Run `tunr doctor`.'));
                }
            }, opts.timeout || 15000);

            const tryExtractURL = (text) => {
                const match = text.match(/https?:\/\/[^\s]+tunr\.dev[^\s]*/);
                if (match && !resolved) {
                    resolved = true;
                    clearTimeout(timeout);
                    const tunnel = new Tunnel(match[0], port, proc);
                    this._activeTunnels.push(tunnel);
                    resolve(tunnel);
                }
            };

            proc.stdout.on('data', (d) => tryExtractURL(d.toString()));
            proc.stderr.on('data', (d) => tryExtractURL(d.toString()));

            proc.on('error', (err) => {
                clearTimeout(timeout);
                reject(new Error(`Failed to start tunr: ${err.message}. Is tunr in your PATH?`));
            });

            proc.on('exit', (code) => {
                clearTimeout(timeout);
                if (!resolved) {
                    reject(new Error(`tunr exited unexpectedly (exit ${code})`));
                }
            });
        });
    }

    /**
     * Share an HTTP server (for Express, Fastify, Next.js)
     * @param {http.Server|Object} server - http.Server or Express app
     * @param {Object} [opts]
     * @param {number} [opts.port=3000]
     * @returns {Promise<string>} Public URL
     * 
     * @example
     * const app = express();
     * const url = await tunr.shareServer(app, { port: 3000 });
     * // url = "https://abc123.tunr.sh"
     */
    async shareServer(server, opts = {}) {
        const port = opts.port || 3000;

        // If it's an Express app, start listening
        if (typeof server.listen === 'function' && !server.listening) {
            await new Promise((resolve) => server.listen(port, resolve));
        }

        const tunnel = await this.share(port, opts);
        return tunnel.publicUrl;
    }

    /**
     * Fetch recent HTTP requests
     * @param {number} [limit=10]
     * @returns {Promise<Array>}
     */
    async requests(limit = 10) {
        return new Promise((resolve, reject) => {
            const reqOpts = {
                hostname: 'localhost',
                port: this.dashPort,
                path: `/api/v1/requests?limit=${limit}`,
                method: 'GET',
            };
            const req = http.request(reqOpts, (res) => {
                let data = '';
                res.on('data', (d) => (data += d));
                res.on('end', () => {
                    try {
                        resolve(JSON.parse(data).requests || []);
                    } catch {
                        resolve([]);
                    }
                });
            });
            req.on('error', () => resolve([]));
            req.end();
        });
    }

    /**
     * Close all active tunnels
     */
    async closeAll() {
        await Promise.all(this._activeTunnels.map((t) => t.close()));
        this._activeTunnels = [];
    }
}

/**
 * An active tunnel
 */
class Tunnel {
    constructor(publicUrl, localPort, process) {
        this.publicUrl = publicUrl;
        this.localPort = localPort;
        this._process = process;
        this._closed = false;

        // Mark as closed when the process exits
        process.on('exit', () => { this._closed = true; });
    }

    /** Tunnel URL */
    get url() { return this.publicUrl; }

    /** Is the tunnel alive? */
    get isAlive() { return !this._closed; }

    /** Close the tunnel */
    async close() {
        if (this._process && !this._closed) {
            this._process.kill('SIGTERM');
            this._closed = true;
        }
    }
}

module.exports = { TunrClient, Tunnel };

// ─── Jest/Vitest Test Helper ──────────────────────────────────────────────────

/**
 * Jest/Vitest integration test helper
 * 
 * @example
 * // jest.setup.js:
 * import { withTunnel } from '@tunr/sdk/test';
 * 
 * describe('Webhook test', () => {
 *   let tunnelURL;
 *   
 *   beforeAll(async () => {
 *     tunnelURL = await withTunnel(3000, async (url) => {
 *       // Set up the Stripe/Paddle webhook endpoint
 *       await stripe.webhooks.update({ url });
 *     });
 *   });
 * });
 */
async function withTunnel(port, callback) {
    const client = new TunrClient();
    const tunnel = await client.share(port);
    try {
        await callback(tunnel.publicUrl, tunnel);
    } finally {
        await tunnel.close();
    }
}

module.exports.withTunnel = withTunnel;
