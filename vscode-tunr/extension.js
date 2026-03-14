// tunr VS Code Extension
// Manage tunnels directly from your editor.
// The status bar shows the URL and lets you copy it.

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
 * Extension activation
 * @param {vscode.ExtensionContext} context 
 */
function activate(context) {
    console.log('tunr extension active!');

    // Status bar item
    statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    statusBarItem.command = 'tunr.copyURL';
    statusBarItem.tooltip = "tunr tunnel — click to copy URL";
    updateStatusBar('idle');
    statusBarItem.show();

    // Tree view providers
    tunnelProvider = new TunnelTreeProvider();
    requestProvider = new RequestTreeProvider();

    vscode.window.registerTreeDataProvider('tunr.tunnelView', tunnelProvider);
    vscode.window.registerTreeDataProvider('tunr.requestView', requestProvider);

    // Register commands
    const commands = [
        vscode.commands.registerCommand('tunr.share', cmdShare),
        vscode.commands.registerCommand('tunr.stop', cmdStop),
        vscode.commands.registerCommand('tunr.status', cmdStatus),
        vscode.commands.registerCommand('tunr.openDashboard', cmdOpenDashboard),
        vscode.commands.registerCommand('tunr.copyURL', cmdCopyURL),
    ];

    commands.forEach(c => context.subscriptions.push(c));
    context.subscriptions.push(statusBarItem);

    // Auto-start based on config
    const config = vscode.workspace.getConfiguration('tunr');
    if (config.get('autoStart') && config.get('defaultPort')) {
        startTunnel(config.get('defaultPort'));
    }

    // Poll tunnel list from dashboard
    startPolling();
}

// ── COMMANDS ──────────────────────────────────────────────────────────────

async function cmdShare() {
    const config = vscode.workspace.getConfiguration('tunr');
    const defaultPort = config.get('defaultPort', 3000);

    // Ask the user for a port
    const portInput = await vscode.window.showInputBox({
        prompt: "Which port do you want to share?",
        value: String(defaultPort),
        validateInput: (v) => {
            const n = parseInt(v, 10);
            return (n >= 1024 && n <= 65535) ? null : "Enter a port between 1024 and 65535";
        },
    });

    if (!portInput) return;
    const port = parseInt(portInput, 10);

    await startTunnel(port);
}

async function startTunnel(port) {
    if (activeTunnel) {
        const stop = await vscode.window.showWarningMessage(
            `Port ${activeTunnel.port} is already active. Stop it first.`,
            "Stop and Start New",
            "Cancel",
        );
        if (stop !== "Stop and Start New") return;
        await cmdStop();
    }

    const binary = getBinaryPath();

    updateStatusBar('connecting', port);
    vscode.window.showInformationMessage(`⌛ Starting tunnel (port ${port})...`);

    // tunr share --port X --no-open
    const proc = spawn(binary, ['share', '--port', String(port), '--no-open'], {
        stdio: ['ignore', 'pipe', 'pipe'],
    });

    let urlFound = false;

    proc.stdout.on('data', (data) => {
        const text = data.toString();
        // Parse URL from stdout
        const match = text.match(/https?:\/\/[^\s]+tunr\.dev[^\s]*/);
        if (match && !urlFound) {
            urlFound = true;
            const url = match[0];
            activeTunnel = { id: generateID(), url, port, process: proc };
            updateStatusBar('active', port, url);

            // Show notification
            vscode.window.showInformationMessage(
                `✅ Tunnel active! ${url}`,
                "📋 Copy",
                "🌐 Open",
            ).then(action => {
                if (action === "📋 Copy") {
                    vscode.env.clipboard.writeText(url);
                } else if (action === "🌐 Open") {
                    vscode.env.openExternal(vscode.Uri.parse(url));
                }
            });

            tunnelProvider?.refresh();
        }
    });

    proc.stderr.on('data', (data) => {
        const text = data.toString();
        // tunr writes logs to stderr; show for visibility
        console.log('[tunr]', text.trim());

        // URL may also appear on stderr
        const match = text.match(/https?:\/\/[^\s]+tunr\.dev[^\s]*/);
        if (match && !urlFound) {
            urlFound = true;
            const url = match[0];
            activeTunnel = { id: generateID(), url, port, process: proc };
            updateStatusBar('active', port, url);
            vscode.window.showInformationMessage(`✅ Tunnel active: ${url}`, "📋 Copy").then(a => {
                if (a === "📋 Copy") vscode.env.clipboard.writeText(url);
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
                vscode.window.showErrorMessage(`tunr tunnel stopped (exit ${code}). Run 'tunr doctor'.`);
            }
        }
    });

    // Show error if URL is not discovered within 15s
    setTimeout(() => {
        if (!urlFound && activeTunnel?.process === proc) {
            vscode.window.showErrorMessage("Tunnel did not start within 15 seconds. Run 'tunr doctor'.");
            proc.kill();
            activeTunnel = null;
            updateStatusBar('idle');
        }
    }, 15_000);
}

async function cmdStop() {
    if (!activeTunnel) {
        vscode.window.showInformationMessage("No active tunnel.");
        return;
    }

    const { port, url, process: proc } = activeTunnel;
    proc?.kill('SIGTERM');
    activeTunnel = null;
    updateStatusBar('idle');
    tunnelProvider?.refresh();
    vscode.window.showInformationMessage(`Tunnel stopped (port ${port}, ${url})`);
}

function cmdStatus() {
    if (!activeTunnel) {
        vscode.window.showInformationMessage("No active tunnel. Start one with tunr.share.");
        return;
    }
    vscode.window.showInformationMessage(
        `✅ Tunnel active\nPort: ${activeTunnel.port}\nURL: ${activeTunnel.url}`,
        "📋 Copy"
    ).then(a => { if (a === "📋 Copy") vscode.env.clipboard.writeText(activeTunnel.url); });
}

function cmdOpenDashboard() {
    const config = vscode.workspace.getConfiguration('tunr');
    const port = config.get('dashboardPort', 19842);
    vscode.env.openExternal(vscode.Uri.parse(`http://localhost:${port}/inspector.html`));
}

function cmdCopyURL() {
    if (!activeTunnel) {
        vscode.window.showInformationMessage("No URL to copy. Start a tunnel first.");
        return;
    }
    vscode.env.clipboard.writeText(activeTunnel.url);
    vscode.window.showInformationMessage("URL copied! " + activeTunnel.url);
}

// ── STATUS BAR ────────────────────────────────────────────────────────────

function updateStatusBar(state, port, url) {
    switch (state) {
        case 'idle':
            statusBarItem.text = '$(broadcast) tunr';
            statusBarItem.backgroundColor = undefined;
            statusBarItem.tooltip = "tunr — no active tunnel. Click or run tunr.share.";
            break;
        case 'connecting':
            statusBarItem.text = `$(loading~spin) tunr :${port}`;
            statusBarItem.tooltip = `Connecting... (port ${port})`;
            break;
        case 'active':
            statusBarItem.text = `$(broadcast) :${port} → active`;
            statusBarItem.tooltip = `Tunnel active: ${url}\nClick to copy URL`;
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
            return [new vscode.TreeItem("No active tunnel", vscode.TreeItemCollapsibleState.None)];
        }

        const item = new vscode.TreeItem(
            `$(broadcast) :${activeTunnel.port} → active`,
            vscode.TreeItemCollapsibleState.None
        );
        item.description = activeTunnel.url;
        item.tooltip = `URL: ${activeTunnel.url}\nPort: ${activeTunnel.port}`;
        item.command = { command: "tunr.copyURL", title: "Copy URL" };
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
            return [new vscode.TreeItem("No requests yet", vscode.TreeItemCollapsibleState.None)];
        }

        return this.requests.slice(0, 20).map(r => {
            const label = `${r.method} ${r.path}`;
            const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.None);
            item.description = `${r.status_code} • ${r.duration_ms}ms`;
            item.tooltip = `${r.method} ${r.path}\nStatus: ${r.status_code}\nDuration: ${r.duration_ms}ms`;
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

// Pull latest requests from dashboard
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
        }).on('error', () => { }); // Silently skip if dashboard is unavailable
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
