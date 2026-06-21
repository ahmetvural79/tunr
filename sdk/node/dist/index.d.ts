import { ChildProcess } from 'child_process';
import { EventEmitter } from 'events';
export interface TunnelOptions {
    subdomain?: string;
    authToken?: string;
    allowIps?: string[];
    qr?: boolean;
    demo?: boolean;
    freeze?: boolean;
    injectWidget?: boolean;
    password?: string;
    xForwardedFor?: boolean;
    corsOrigins?: string[];
    region?: string;
    headerAdd?: string[];
    headerRemove?: string[];
    proxy?: string;
    ttl?: string;
}
export interface Request {
    id: string;
    method: string;
    path: string;
    status_code: number;
    duration_ms: number;
    timestamp: string;
    req_body: string;
    resp_body: string;
    req_headers: Record<string, string>;
}
export interface ActiveTunnel {
    id: string;
    subdomain: string;
    local_port: number;
    status: 'active' | 'closed';
    public_url: string;
    created_at: string;
    protocol: string;
}
export declare class Tunnel extends EventEmitter {
    publicUrl: string;
    localPort: number;
    subdomain?: string;
    protocol: string;
    private child?;
    constructor(publicUrl: string, localPort: number, child?: ChildProcess, subdomain?: string, protocol?: string);
    close(): Promise<void>;
}
export interface TunrClientOptions {
    apiToken?: string;
    relayUrl?: string;
    inspectorUrl?: string;
}
export declare class TunrClient {
    private apiToken?;
    private relayUrl;
    private inspectorUrl;
    constructor(opts?: TunrClientOptions);
    private buildArgs;
    private startTunnel;
    share(port: number, opts?: TunnelOptions): Promise<Tunnel>;
    tcp(port: number, opts?: TunnelOptions): Promise<Tunnel>;
    udp(port: number, opts?: TunnelOptions): Promise<Tunnel>;
    tls(port: number, opts?: TunnelOptions): Promise<Tunnel>;
    getActiveTunnels(): Promise<ActiveTunnel[]>;
    getRequests(subdomain: string, limit?: number): Promise<Request[]>;
    replayRequest(subdomain: string, requestId: string, port: number): Promise<void>;
    getMetrics(): Promise<string>;
    healthCheck(): Promise<any>;
    private httpGet;
    private httpPost;
}
//# sourceMappingURL=index.d.ts.map