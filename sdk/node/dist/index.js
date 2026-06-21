"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.TunrClient = exports.Tunnel = void 0;
const child_process_1 = require("child_process");
const events_1 = require("events");
const http = __importStar(require("http"));
const https = __importStar(require("https"));
class Tunnel extends events_1.EventEmitter {
    constructor(publicUrl, localPort, child, subdomain, protocol = 'http') {
        super();
        this.publicUrl = publicUrl;
        this.localPort = localPort;
        this.subdomain = subdomain;
        this.protocol = protocol;
        this.child = child;
    }
    async close() {
        return new Promise((resolve) => {
            if (this.child) {
                const child = this.child;
                if (child.exitCode === null) {
                    child.on('exit', () => {
                        this.emit('close');
                        resolve();
                    });
                    child.kill('SIGTERM');
                }
                else {
                    this.emit('close');
                    resolve();
                }
            }
            else {
                this.emit('close');
                resolve();
            }
        });
    }
}
exports.Tunnel = Tunnel;
class TunrClient {
    constructor(opts) {
        this.apiToken = opts?.apiToken;
        this.relayUrl = opts?.relayUrl ?? 'https://relay.tunr.sh';
        this.inspectorUrl = opts?.inspectorUrl ?? 'http://localhost:19842';
    }
    buildArgs(command, port, opts) {
        const args = [command, '--port', String(port), '--no-open'];
        if (opts?.subdomain)
            args.push('--subdomain', opts.subdomain);
        if (opts?.authToken)
            args.push('--auth-token', opts.authToken);
        if (opts?.password)
            args.push('--password', opts.password);
        if (opts?.qr)
            args.push('--qr');
        if (opts?.demo)
            args.push('--demo');
        if (opts?.freeze)
            args.push('--freeze');
        if (opts?.injectWidget)
            args.push('--inject-widget');
        if (opts?.xForwardedFor)
            args.push('--x-forwarded-for');
        if (opts?.proxy)
            args.push('--proxy', opts.proxy);
        if (opts?.ttl)
            args.push('--ttl', opts.ttl);
        const allowIps = opts?.allowIps ?? [];
        for (const ip of allowIps) {
            args.push('--allow-ip', ip);
        }
        const corsOrigins = opts?.corsOrigins ?? [];
        for (const origin of corsOrigins) {
            args.push('--cors-origin', origin);
        }
        const headerAdd = opts?.headerAdd ?? [];
        for (const header of headerAdd) {
            args.push('--header-add', header);
        }
        const headerRemove = opts?.headerRemove ?? [];
        for (const header of headerRemove) {
            args.push('--header-remove', header);
        }
        if (opts?.region)
            args.push('--region', opts.region);
        return args;
    }
    startTunnel(command, port, opts, protocol = 'http') {
        if (port < 1024 || port > 65535) {
            throw new Error(`Invalid port: ${port}`);
        }
        const args = this.buildArgs(command, port, opts);
        return new Promise((resolve, reject) => {
            let urlFound = false;
            const timeout = setTimeout(() => {
                if (!urlFound) {
                    child.kill('SIGTERM');
                    reject(new Error(`${command} tunnel URL not found within 10s`));
                }
            }, 10000);
            const child = (0, child_process_1.spawn)('tunr', args, {
                stdio: ['inherit', 'pipe', 'pipe'],
                env: { ...process.env }
            });
            const handleOutput = (data) => {
                const text = typeof data === 'string' ? data : data.toString();
                const match = text.match(/(https?:\/\/[a-zA-Z0-9._-]+tunr\.sh(?:\/[^\s]*)?|tcp:\/\/[^\s]+)/);
                if (match && match[1]) {
                    urlFound = true;
                    clearTimeout(timeout);
                    resolve(new Tunnel(match[1], port, child, opts?.subdomain, protocol));
                }
            };
            child.stdout?.on('data', handleOutput);
            child.stderr?.on('data', handleOutput);
            child.on('exit', (code) => {
                if (!urlFound) {
                    clearTimeout(timeout);
                    reject(new Error(`tunr exited unexpectedly with code ${code}. Use 'tunr --help' for options.`));
                }
            });
        });
    }
    async share(port, opts) {
        return this.startTunnel('share', port, opts, 'http');
    }
    async tcp(port, opts) {
        return this.startTunnel('tcp', port, opts, 'tcp');
    }
    async udp(port, opts) {
        return this.startTunnel('udp', port, opts, 'udp');
    }
    async tls(port, opts) {
        return this.startTunnel('tls', port, opts, 'tls');
    }
    async getActiveTunnels() {
        const data = await this.httpGet(`${this.relayUrl}/api/v1/tunnels`);
        return data?.tunnels ?? [];
    }
    async getRequests(subdomain, limit = 50) {
        const params = new URLSearchParams({ limit: String(limit) });
        const data = await this.httpGet(`${this.relayUrl}/api/v1/tunnels/${subdomain}/requests?${params}`);
        return data?.requests ?? [];
    }
    async replayRequest(subdomain, requestId, port) {
        await this.httpPost(`${this.relayUrl}/api/v1/tunnels/${subdomain}/requests/${requestId}/replay`, { port });
    }
    async getMetrics() {
        return new Promise((resolve, reject) => {
            http.get(`${this.inspectorUrl}/metrics`, (res) => {
                let body = '';
                res.on('data', (d) => { body += d; });
                res.on('end', () => resolve(body));
            }).on('error', reject);
        });
    }
    async healthCheck() {
        return this.httpGet(`${this.inspectorUrl}/healthz`);
    }
    async httpGet(url) {
        const protocol = url.startsWith('https') ? https : http;
        return new Promise((resolve, reject) => {
            const headers = {};
            if (this.apiToken) {
                headers['Authorization'] = `Bearer ${this.apiToken}`;
            }
            const req = protocol
                .get(url, { headers }, (res) => {
                let body = '';
                res.on('data', (d) => {
                    body += d;
                });
                res.on('end', () => {
                    try {
                        resolve(JSON.parse(body));
                    }
                    catch {
                        reject(new Error(`Unexpected JSON response: ${body}`));
                    }
                });
            });
            req.on('error', reject);
        });
    }
    async httpPost(url, body) {
        const protocol = url.startsWith('https') ? https : http;
        const jsonBody = JSON.stringify(body);
        const headers = {
            'Content-Type': 'application/json',
            'Content-Length': String(Buffer.byteLength(jsonBody)),
        };
        if (this.apiToken) {
            headers['Authorization'] = `Bearer ${this.apiToken}`;
        }
        return new Promise((resolve, reject) => {
            const client = protocol;
            const req = client.request(url, {
                method: 'POST',
                headers,
            }, (res) => {
                let body = '';
                res.on('data', (d) => {
                    body += d;
                });
                res.on('end', () => {
                    let data = {};
                    try {
                        data = JSON.parse(body);
                    }
                    catch { }
                    resolve(data);
                });
            });
            req.on('error', reject);
            req.write(jsonBody);
            req.end();
        });
    }
}
exports.TunrClient = TunrClient;
//# sourceMappingURL=index.js.map