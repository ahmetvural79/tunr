import { spawn, ChildProcess } from 'child_process'
import { EventEmitter } from 'events'
import * as http from 'http'
import * as https from 'https'

export interface TunnelOptions {
  subdomain?: string
  authToken?: string
  allowIps?: string[]
  qr?: boolean
  demo?: boolean
  password?: string
  xForwardedFor?: boolean
  corsOrigins?: string[]
  region?: string
}

export interface Request {
  id: string
  method: string
  path: string
  status_code: number
  duration_ms: number
  timestamp: string
  req_body: string
  resp_body: string
  req_headers: Record<string, string>
}

export interface ActiveTunnel {
  id: string
  subdomain: string
  local_port: number
  status: 'active' | 'closed'
  public_url: string
  created_at: string
}

export class Tunnel extends EventEmitter {
  publicUrl: string
  localPort: number
  subdomain?: string
  private child?: ChildProcess

  constructor(
    publicUrl: string,
    localPort: number,
    child?: ChildProcess,
    subdomain?: string
  ) {
    super()
    this.publicUrl = publicUrl
    this.localPort = localPort
    this.subdomain = subdomain
    this.child = child
  }

  async close(): Promise<void> {
    return new Promise((resolve) => {
      if (this.child) {
        const child = this.child as ChildProcess
        if (child.exitCode === null) {
          child.on('exit', () => {
            this.emit('close')
            resolve()
          })
          child.kill('SIGTERM')
        } else {
          this.emit('close')
          resolve()
        }
      } else {
        this.emit('close')
        resolve()
      }
    })
  }
}

export interface TunrClientOptions {
  apiToken?: string
  relayUrl?: string
  inspectorUrl?: string
}

export class TunrClient {
  private apiToken?: string
  private relayUrl: string
  private inspectorUrl: string

  constructor(opts?: TunrClientOptions) {
    this.apiToken = opts?.apiToken
    this.relayUrl = opts?.relayUrl ?? 'https://relay.tunr.sh'
    this.inspectorUrl = opts?.inspectorUrl ?? 'http://localhost:19842'
  }

  async share(port: number, opts?: TunnelOptions): Promise<Tunnel> {
    if (port < 1024 || port > 65535) {
      throw new Error(`Invalid port: ${port}`)
    }

    const args = ['share', '--port', String(port), '--no-open']

    if (opts?.subdomain) args.push('--subdomain', opts.subdomain)
    if (opts?.authToken) args.push('--auth-token', opts.authToken)
    if (opts?.password) args.push('--password', opts.password)
    if (opts?.qr) args.push('--qr')
    if (opts?.demo) args.push('--demo')
    if (opts?.xForwardedFor) args.push('--x-forwarded-for')

    const allowIps = opts?.allowIps ?? []
    for (const ip of allowIps) {
      args.push('--allow-ip', ip)
    }

    const corsOrigins = opts?.corsOrigins ?? []
    for (const origin of corsOrigins) {
      args.push('--cors-origin', origin)
    }

    if (opts?.region) args.push('--region', opts.region)

    return new Promise<T>((resolve, reject) => {
      let urlFound = false
      const timeout = setTimeout(() => {
        if (!urlFound) {
          child.kill('SIGTERM')
          reject(new Error('Tunnel URL not found within 10s'))
        }
      }, 10000)

      const child = spawn('tunr', args, {
        stdio: ['inherit', 'pipe', 'pipe'],
        env: { ...process.env }
      })

      const handleOutput = (data: Buffer | string) => {
        const text = typeof data === 'string' ? data : data.toString()
        const match = text.match(
          /(https?:\/\/[a-zA-Z0-9._-]+tunr\.sh(?:\/[^\s]*)?)/
        )
        if (match && match[1]) {
          urlFound = true
          clearTimeout(timeout)
          resolve(
            new Tunnel(match[1], port, child, opts?.subdomain)
          )
        }
      }

      child.stdout?.on('data', handleOutput)
      child.stderr?.on('data', handleOutput)
      child.on('exit', (code) => {
        if (!urlFound) {
          clearTimeout(timeout)
          reject(
            new Error(
              `tunr exited unexpectedly with code ${code}. Use 'tunr --help' for options.`
            )
          )
        }
      })
    })
  }

  async getActiveTunnels(): Promise<ActiveTunnel[]> {
    const data = await this.httpGet(`${this.relayUrl}/api/v1/tunnels`)
    return data?.tunnels ?? []
  }

  async getRequests(
    subdomain: string,
    limit = 50
  ): Promise<Request[]> {
    const params = new URLSearchParams({ limit: String(limit)})
    const data = await this.httpGet(
      `${this.relayUrl}/api/v1/tunnels/${subdomain}/requests?${params}`
    )
    return data?.requests ?? []
  }

  async replayRequest(
    subdomain: string,
    requestId: string,
    port: number
  ): Promise<void> {
    await this.httpPost(
      `${this.relayUrl}/api/v1/tunnels/${subdomain}/requests/${requestId}/replay`,
      { port }
    )
  }

  private async httpGet(url: string): Promise<any> {
    const protocol: typeof http | typeof https =
      url.startsWith('https') ? https : http

    return new Promise((resolve, reject) => {
      const headers: Record<string, string> = {}
      if (this.apiToken) {
        headers['Authorization'] = `Bearer ${this.apiToken}`
      }

      const req = protocol
        .get(url, { headers }, (res) => {
          let body = ''
          res.on('data', (d: Buffer | string) => {
            body += d
          })
          res.on('end', () => {
            try {
              resolve(JSON.parse(body))
            } catch {
              reject(new Error(`Unexpected JSON response: ${body}`))
            }
          })
        })
      req.on('error', reject)
    })
  }

  private async httpPost(url: string, body: unknown): Promise<any> {
    const protocol: typeof http | typeof https =
      url.startsWith('https') ? https : http

    const jsonBody = JSON.stringify(body)
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      'Content-Length': String(Buffer.byteLength(jsonBody)),
    }

    if (this.apiToken) {
      headers['Authorization'] = `Bearer ${this.apiToken}`
    }

    return new Promise((resolve, reject) => {
      const client = protocol
      const req = client.request(url, {
        method: 'POST',
        headers,
      }, (res) => {
        let body = ''
        res.on('data', (d: Buffer | string) => {
          body += d
        })
        res.on('end', () => {
          let data: any = {}
          try {
            data = JSON.parse(body)
          } catch {}
          resolve(data)
        })
      })
      req.on('error', reject)
      req.write(jsonBody)
      req.end()
    })
  }
}
