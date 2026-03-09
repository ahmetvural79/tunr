// tunr VS Code Extension
// Tunnel'ları doğrudan editörden yönetin.
// Status bar'da URL görür, tıklarsınız, kopyalanır. Basit ve güzel.

const vscode = require('vscode');
const { execFile, spawn } = require('child_process');
const http = require('http');

// ── State ──
let activeTunnel = null;   // { id, url, port, process }
let statusBarItem = null;
let tunnelProvider = null;
let requestProvider = null;
let pollTimer = null;

/**
 * Extension aktivasyonu
 * @param {vscode.ExtensionContext} context 
 */
function activate(context) {
    console.log('tunr extension aktif!');

    // Status bar item
    statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    statusBarItem.command = 'tunr.copyURL';
    statusBarItem.tooltip = 'tunr tunnel — tıkla URL\'yi kopyala';
    updateStatusBar('idle');
    statusBarItem.show();

    // Tree view providers
    tunnelProvider = new TunnelTreeProvider();
    requestProvider = new RequestTreeProvider();

    vscode.window.registerTreeDataProvider('tunr.tunnelView', tunnelProvider);
    vscode.window.registerTreeDataProvider('tunr.requestView', requestProvider);

    // Komutları kaydet
    const commands = [
        vscode.commands.registerCommand('tunr.share', cmdShare),
        vscode.commands.registerCommand('tunr.stop', cmdStop),
        vscode.commands.registerCommand('tunr.status', cmdStatus),
        vscode.commands.registerCommand('tunr.openDashboard', cmdOpenDashboard),
        vscode.commands.registerCommand('tunr.copyURL', cmdCopyURL),
    ];

    commands.forEach(c => context.subscriptions.push(c));
    context.subscriptions.push(statusBarItem);

    // Config'e göre autoStart
    const config = vscode.workspace.getConfiguration('tunr');
    if (config.get('autoStart') && config.get('defaultPort')) {
        startTunnel(config.get('defaultPort'));
    }

    // Polling — dashboard'dan tünel listesi al
    startPolling();
}

// ── COMMANDS ──────────────────────────────────────────────────────────────

async function cmdShare() {
    const config = vscode.workspace.getConfiguration('tunr');
    const defaultPort = config.get('defaultPort', 3000);

    // Port'u kullanıcıdan sor
    const portInput = await vscode.window.showInputBox({
        prompt: 'Hangi port\'u paylaşmak istiyorsunuz?',
        value: String(defaultPort),
        validateInput: (v) => {
            const n = parseInt(v, 10);
            return (n >= 1024 && n <= 65535) ? null : '1024-65535 arası bir port girin';
        },
    });

    if (!portInput) return;
    const port = parseInt(portInput, 10);

    await startTunnel(port);
}

async function startTunnel(port) {
    if (activeTunnel) {
        const stop = await vscode.window.showWarningMessage(
            `Port ${activeTunnel.port} zaten aktif. Önce durdurun.`,
            'Durdur ve Yeni Aç',
            'İptal',
        );
        if (stop !== 'Durdur ve Yeni Aç') return;
        await cmdStop();
    }

    const binary = getBinaryPath();

    updateStatusBar('connecting', port);
    vscode.window.showInformationMessage(`⌛ Tunnel başlatılıyor (port ${port})...`);

    // tunr share --port X --no-open
    const proc = spawn(binary, ['share', '--port', String(port), '--no-open'], {
        stdio: ['ignore', 'pipe', 'pipe'],
    });

    let urlFound = false;

    proc.stdout.on('data', (data) => {
        const text = data.toString();
        // URL'yi stdout'tan parse et
        const match = text.match(/https?:\/\/[^\s]+tunr\.dev[^\s]*/);
        if (match && !urlFound) {
            urlFound = true;
            const url = match[0];
            activeTunnel = { id: generateID(), url, port, process: proc };
            updateStatusBar('active', port, url);

            // Bildirim göster
            vscode.window.showInformationMessage(
                `✅ Tunnel aktif! ${url}`,
                '📋 Kopyala', '🌐 Aç',
            ).then(action => {
                if (action === '📋 Kopyala') {
                    vscode.env.clipboard.writeText(url);
                } else if (action === '🌐 Aç') {
                    vscode.env.openExternal(vscode.Uri.parse(url));
                }
            });

            tunnelProvider?.refresh();
        }
    });

    proc.stderr.on('data', (data) => {
        const text = data.toString();
        // tunr stderr'e log yazar, bilgi amaçlı göster
        console.log('[tunr]', text.trim());

        // URL stderr'de de olabilir
        const match = text.match(/https?:\/\/[^\s]+tunr\.dev[^\s]*/);
        if (match && !urlFound) {
            urlFound = true;
            const url = match[0];
            activeTunnel = { id: generateID(), url, port, process: proc };
            updateStatusBar('active', port, url);
            vscode.window.showInformationMessage(`✅ Tunnel aktif: ${url}`, '📋 Kopyala').then(a => {
                if (a === '📋 Kopyala') vscode.env.clipboard.writeText(url);
            });
            tunnelProvider?.refresh();
        }
    });

    proc.on('exit', (code) => {
        if (activeTunnel?.process === proc) {
            activeTunnel = null;
            updateStatusBar('idle');
            tunnelProvider?.refresh();
            if (code !== 0) {
                vscode.window.showErrorMessage(`tunr tunnel kapandı (exit ${code}). 'tunr doctor' çalıştırın.`);
            }
        }
    });

    // 15s içinde URL gelmezse hata ver
    setTimeout(() => {
        if (!urlFound && activeTunnel?.process === proc) {
            vscode.window.showErrorMessage('Tunnel 15 saniyede başlamadı. tunr doctor çalıştırın.');
            proc.kill();
            activeTunnel = null;
            updateStatusBar('idle');
        }
    }, 15_000);
}

async function cmdStop() {
    if (!activeTunnel) {
        vscode.window.showInformationMessage('Aktif tunnel yok.');
        return;
    }

    const { port, url, process: proc } = activeTunnel;
    proc?.kill('SIGTERM');
    activeTunnel = null;
    updateStatusBar('idle');
    tunnelProvider?.refresh();
    vscode.window.showInformationMessage(`Tunnel kapatıldı (port ${port}, ${url})`);
}

function cmdStatus() {
    if (!activeTunnel) {
        vscode.window.showInformationMessage('Aktif tunnel yok. tunr.share ile başlatın.');
        return;
    }
    vscode.window.showInformationMessage(
        `✅ Tunnel aktif\nPort: ${activeTunnel.port}\nURL: ${activeTunnel.url}`,
        '📋 Kopyala'
    ).then(a => { if (a === '📋 Kopyala') vscode.env.clipboard.writeText(activeTunnel.url); });
}

function cmdOpenDashboard() {
    const config = vscode.workspace.getConfiguration('tunr');
    const port = config.get('dashboardPort', 19842);
    vscode.env.openExternal(vscode.Uri.parse(`http://localhost:${port}/inspector.html`));
}

function cmdCopyURL() {
    if (!activeTunnel) {
        vscode.window.showInformationMessage('Kopyalanacak URL yok. Önce tunnel başlatın.');
        return;
    }
    vscode.env.clipboard.writeText(activeTunnel.url);
    vscode.window.showInformationMessage('URL kopyalandı! ' + activeTunnel.url);
}

// ── STATUS BAR ────────────────────────────────────────────────────────────

function updateStatusBar(state, port, url) {
    switch (state) {
        case 'idle':
            statusBarItem.text = '$(broadcast) tunr';
            statusBarItem.backgroundColor = undefined;
            statusBarItem.tooltip = 'tunr — tunnel yok. Tıkla veya tunr.share çalıştır.';
            break;
        case 'connecting':
            statusBarItem.text = `$(loading~spin) tunr :${port}`;
            statusBarItem.tooltip = `Bağlanıyor... (port ${port})`;
            break;
        case 'active':
            statusBarItem.text = `$(broadcast) :${port} → aktif`;
            statusBarItem.tooltip = `Tunnel aktif: ${url}\nTıkla URL'yi kopyala`;
            statusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.prominentBackground');
            break;
    }
}

// ── TREE PROVIDERS ────────────────────────────────────────────────────────

class TunnelTreeProvider {
    constructor() {
        this._onDidChangeTreeData = new vscode.EventEmitter();
        this.onDidChangeTreeData = this._onDidChangeTreeData.event;
    }

    refresh() { this._onDidChangeTreeData.fire(); }

    getTreeItem(element) { return element; }

    getChildren(element) {
        if (element) return [];

        if (!activeTunnel) {
            return [new vscode.TreeItem('Aktif tunnel yok', vscode.TreeItemCollapsibleState.None)];
        }

        const item = new vscode.TreeItem(
            `$(broadcast) :${activeTunnel.port} → aktif`,
            vscode.TreeItemCollapsibleState.None
        );
        item.description = activeTunnel.url;
        item.tooltip = `URL: ${activeTunnel.url}\nPort: ${activeTunnel.port}`;
        item.command = { command: 'tunr.copyURL', title: 'URL Kopyala' };
        return [item];
    }
}

class RequestTreeProvider {
    constructor() {
        this._onDidChangeTreeData = new vscode.EventEmitter();
        this.onDidChangeTreeData = this._onDidChangeTreeData.event;
        this.requests = [];
    }

    refresh(requests) {
        this.requests = requests || [];
        this._onDidChangeTreeData.fire();
    }

    getTreeItem(el) { return el; }

    getChildren() {
        if (!this.requests.length) {
            return [new vscode.TreeItem('Henüz istek yok', vscode.TreeItemCollapsibleState.None)];
        }

        return this.requests.slice(0, 20).map(r => {
            const label = `${r.method} ${r.path}`;
            const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.None);
            item.description = `${r.status_code} • ${r.duration_ms}ms`;
            item.tooltip = `${r.method} ${r.path}\nStatus: ${r.status_code}\nSüre: ${r.duration_ms}ms`;
            return item;
        });
    }
}

// ── HELPERS ───────────────────────────────────────────────────────────────

function getBinaryPath() {
    const config = vscode.workspace.getConfiguration('tunr');
    return config.get('binaryPath', 'tunr');
}

function generateID() {
    return Math.random().toString(36).slice(2, 10);
}

// Dashboard'dan son istekleri al
function startPolling() {
    pollTimer = setInterval(() => {
        if (!activeTunnel) return;

        const config = vscode.workspace.getConfiguration('tunr');
        const dashPort = config.get('dashboardPort', 19842);

        http.get(`http://localhost:${dashPort}/api/v1/requests?limit=20`, (res) => {
            let data = '';
            res.on('data', (d) => data += d);
            res.on('end', () => {
                try {
                    const parsed = JSON.parse(data);
                    requestProvider?.refresh(parsed.requests || []);
                } catch { }
            });
        }).on('error', () => { }); // dashboard çalışmıyorsa sessizce geç
    }, 3000);
}

function deactivate() {
    clearInterval(pollTimer);
    if (activeTunnel?.process) {
        activeTunnel.process.kill('SIGTERM');
        activeTunnel = null;
    }
}

module.exports = { activate, deactivate };
