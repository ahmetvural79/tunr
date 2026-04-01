'''Client module for tunr SDK.'''

from dataclasses import dataclass, field
import httpx
import subprocess
import re
import threading
from typing import Any


@dataclass
class Tunnel:
    public_url: str | None = None
    local_port: int | None = None
    subdomain: str | None = None
    _process: subprocess.Popen = field(default=None, repr=False)

    def close(self) -> None:
        if self._process and self._process.poll() is None:
            self._process.terminate()
            try:
                self._process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self._process.kill()

    @property
    def is_alive(self) -> bool:
        return self._process and self._process.poll() is None


class TunnelOptions:
    def __init__(
        self,
        subdomain: str | None = None,
        auth_token: str | None = None,
        allow_ips: list[str] | None = None,
        qr: bool = False,
        demo: bool = False,
        password: str | None = None,
        x_forwarded_for: bool = False,
        cors_origins: list[str] | None = None,
        region: str | None = None,
    ):
        self.subdomain = subdomain
        self.auth_token = auth_token
        self.allow_ips = allow_ips or []
        self.qr = qr
        self.demo = demo
        self.password = password
        self.x_forwarded_for = x_forwarded_for
        self.cors_origins = cors_origins or []
        self.region = region


class TunrClient:
    def __init__(
        self,
        api_token: str | None = None,
        relay_url: str = 'https://relay.tunr.sh',
    ):
        self.api_token = api_token
        self.relay_url = relay_url
        self._http = httpx.Client(
            base_url=self.relay_url,
            timeout=httpx.Timeout(30.0),
        )

    def _headers(self) -> dict[str, str]:
        hdrs = {'Content-Type': 'application/json'}
        if self.api_token:
            hdrs['Authorization'] = f'Bearer {self.api_token}'
        return hdrs

    def share(
        self,
        port: int,
        opts: TunnelOptions | None = None,
    ) -> Tunnel:
        opts = opts or TunnelOptions()

        args = ['tunr', 'share', '--port', str(port), '--no-open']
        if opts.subdomain:
            args.extend(['--subdomain', opts.subdomain])
        if opts.auth_token:
            args.extend(['--auth-token', opts.auth_token])
        if opts.password:
            args.extend(['--password', opts.password])
        if opts.qr:
            args.append('--qr')
        if opts.demo:
            args.append('--demo')
        if opts.x_forwarded_for:
            args.append('--x-forwarded-for')

        for ip in opts.allow_ips:
            args.extend(['--allow-ip', ip])

        for origin in opts.cors_origins:
            args.extend(['--cors-origin', origin])

        if opts.region:
            args.extend(['--region', opts.region])

        proc = subprocess.Popen(
            args,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,
        )

        url_event = threading.Event()
        result = {'url': None}

        def _reader():
            url_re = re.compile(
                r'(https?://[a-zA-Z0-9._-]+tunr\.sh(?:/[^\s]*)?)'
            )
            for line in proc.stdout:
                m = url_re.search(line)
                if m:
                    result['url'] = m.group(1)
                    url_event.set()
                    # Don't break, keep reading so process doesn't get blocked

        t = threading.Thread(target=_reader, daemon=True)
        t.start()

        if not url_event.wait(timeout=10):
            proc.terminate()
            raise RuntimeError('tunnel URL not found within 10 seconds')

        return Tunnel(
            public_url=result['url'],
            local_port=port,
            subdomain=opts.subdomain,
            _process=proc,
        )

    def get_active_tunnels(self) -> list[dict[str, Any]]:
        resp = self._http.get('/api/v1/tunnels', headers=self._headers())
        resp.raise_for_status()
        return resp.json().get('tunnels', [])

    def get_requests(self, subdomain: str, limit: int = 50) -> list[dict[str, Any]]:
        resp = self._http.get(
            f'/api/v1/tunnels/{subdomain}/requests',
            params={'limit': limit},
            headers=self._headers(),
        )
        resp.raise_for_status()
        return resp.json().get('requests', [])

    def replay_request(
        self, subdomain: str, request_id: str, port: int
    ) -> dict[str, Any]:
        resp = self._http.post(
            f'/api/v1/tunnels/{subdomain}/requests/{request_id}/replay',
            json={'port': port},
            headers=self._headers(),
        )
        resp.raise_for_status()
        return resp.json()

    def close(self):
        self._http.close()
