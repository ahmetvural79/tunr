/**
 * @tunr/sdk — JavaScript/TypeScript SDK
 * 
 * tunr CLI'ı Node.js uygulamalarından programatik kullanın.
 * 
 * @example
 * import { TunrClient } from '@tunr/sdk';
 * 
 * const tunr = new TunrClient();
 * const tunnel = await tunr.share(3000);
 * console.log(tunnel.publicUrl); // https://abc123.tunr.sh
 * 
 * // Express/Next.js test sunucusu için:
 * const url = await tunr.shareServer(app, { port: 3000 });
 */

'use strict';

const { spawn, exec } = require('child_process');
const http = require('http');
const https = require('https');

class TunrClient {
    /**
     * @param {Object} options
     * @param {string} [options.token] - Auth token (opsiyonel)
     * @param {string} [options.binary='tunr'] - tunr binary yolu
     * @param {number} [options.dashPort=19842] - Inspector dashboard portu
     */
    constructor(options = {}) {
        this.token = options.token;
        this.binary = options.binary || 'tunr';
        this.dashPort = options.dashPort || 19842;
        this._activeTunnels = [];
    }

    /**
     * Local portu public URL olarak paylaş
     * @param {number} port - Local port (örn: 3000)
     * @param {Object} [opts] - Seçenekler
     * @param {string} [opts.subdomain] - Tercih edilen subdomain
     * @param {number} [opts.timeout=15000] - Tunnel başlama süre aşımı (ms)
     * @returns {Promise<Tunnel>}
     */
    async share(port, opts = {}) {
        if (port < 1024 || port > 65535) {
            throw new Error(`Geçersiz port: ${port} (1024-65535 arası)`);
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
                    reject(new Error('Tunnel 15 saniyede başlamadı. `tunr doctor` çalıştırın.'));
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
                reject(new Error(`tunr başlatılamadı: ${err.message}. PATH'te tunr var mı?`));
            });

            proc.on('exit', (code) => {
                clearTimeout(timeout);
                if (!resolved) {
                    reject(new Error(`tunr beklenmedik çıktı (exit ${code})`));
                }
            });
        });
    }

    /**
     * HTTP server'ı paylaş (Express, Fastify, Next.js için)
     * @param {http.Server|Object} server - http.Server veya Express app
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

        // Express app ise dinleme başlat
        if (typeof server.listen === 'function' && !server.listening) {
            await new Promise((resolve) => server.listen(port, resolve));
        }

        const tunnel = await this.share(port, opts);
        return tunnel.publicUrl;
    }

    /**
     * Son HTTP isteklerini getir
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
     * Tüm aktif tunnel'ları kapat
     */
    async closeAll() {
        await Promise.all(this._activeTunnels.map((t) => t.close()));
        this._activeTunnels = [];
    }
}

/**
 * Aktif bir tunnel
 */
class Tunnel {
    constructor(publicUrl, localPort, process) {
        this.publicUrl = publicUrl;
        this.localPort = localPort;
        this._process = process;
        this._closed = false;

        // Process kapanınca işaretle
        process.on('exit', () => { this._closed = true; });
    }

    /** Tunnel URL'si */
    get url() { return this.publicUrl; }

    /** Tunnel aktif mi? */
    get isAlive() { return !this._closed; }

    /** Tunnel'ı kapat */
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
 * Jest/Vitest entegrasyon test yardımcısı
 * 
 * @example
 * // jest.setup.js:
 * import { withTunnel } from '@tunr/sdk/test';
 * 
 * describe('Webhook testi', () => {
 *   let tunnelURL;
 *   
 *   beforeAll(async () => {
 *     tunnelURL = await withTunnel(3000, async (url) => {
 *       // Stripe/Paddle webhook endpoint'ini ayarla
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
