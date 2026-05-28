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
exports.activate = activate;
exports.deactivate = deactivate;
const fs = __importStar(require("fs"));
const http = __importStar(require("http"));
const https = __importStar(require("https"));
const path = __importStar(require("path"));
const vscode = __importStar(require("vscode"));
const node_1 = require("vscode-languageclient/node");
let client;
let outputChannel;
const LANGUAGE_ID = 'bcl';
const WATCHED_FILE_GLOBS = ['**/*.bcl', '**/*.schema'];
const TRUSTED_COMMANDS = [
    'bcl.compileCurrentFile',
    'bcl.explainCurrentFile',
    'bcl.restartLanguageServer'
];
async function activate(context) {
    outputChannel = vscode.window.createOutputChannel('BCL Language Server');
    context.subscriptions.push(outputChannel, vscode.commands.registerCommand('bcl.restartLanguageServer', async () => {
        await restartClient(context, true);
    }), vscode.commands.registerCommand('bcl.showRecentSymbols', async () => {
        if (!client) {
            vscode.window.showWarningMessage('BCL language server is not running.');
            return;
        }
        try {
            const items = await client.sendRequest('bcl/recentSymbols');
            const picked = await vscode.window.showQuickPick(items, { placeHolder: 'Recent BCL symbols' });
            if (picked) {
                await vscode.commands.executeCommand('workbench.action.quickOpen', `#${picked.label}`);
            }
        }
        catch (error) {
            reportError('BCL recent symbols failed', error);
        }
    }), vscode.commands.registerCommand('bcl.validateWorkspace', () => runBclCommand(['validate', '--strict', workspacePath()])), vscode.commands.registerCommand('bcl.compileCurrentFile', () => runCurrentFileCommand('compile')), vscode.commands.registerCommand('bcl.explainCurrentFile', () => runCurrentFileCommand('explain')), vscode.commands.registerCommand('bcl.condition.routeCoverage', () => runRouteCoverage()), vscode.commands.registerCommand('bcl.condition.lifecyclePlayground', () => runLifecyclePlayground()), vscode.commands.registerCommand('bcl.condition.compactState', () => runStateCompaction()), vscode.commands.registerCommand('bcl.condition.openRequestLifecycleExample', () => openWorkspaceFile('condition/examples/request-lifecycle/decision.bcl')), vscode.commands.registerCommand('bcl.condition.openHttpAuthGuardExample', () => openWorkspaceFile('condition/examples/http-auth-guard/decision.bcl')), vscode.languages.registerHoverProvider({ language: LANGUAGE_ID, scheme: 'file' }, {
        provideHover: async (document, position) => provideRichHover(document, position)
    }));
    await restartClient(context, false);
}
async function deactivate() {
    await stopClient();
}
async function startClient(context) {
    const command = resolveServerCommand(context);
    output(`Starting BCL language server: ${command.command}${command.args ? ` ${command.args.join(' ')}` : ''}`);
    const serverOptions = command.args
        ? { command: command.command, args: command.args, transport: node_1.TransportKind.stdio, options: { cwd: command.cwd } }
        : { command: command.command, transport: node_1.TransportKind.stdio };
    const clientOptions = {
        documentSelector: [{ scheme: 'file', language: LANGUAGE_ID }],
        synchronize: {
            fileEvents: WATCHED_FILE_GLOBS.map((glob) => vscode.workspace.createFileSystemWatcher(glob))
        },
        middleware: {
            provideHover: async () => null
        },
        initializationOptions: {
            useCustomHoverDetail: true
        },
        outputChannel,
        traceOutputChannel: outputChannel
    };
    client = new node_1.LanguageClient('bcl', 'BCL Language Server', serverOptions, clientOptions);
    await client.start();
}
async function stopClient() {
    if (client) {
        const old = client;
        client = undefined;
        try {
            await old.stop();
        }
        catch (error) {
            output(`Ignoring BCL language server stop error: ${errorMessage(error)}`);
        }
    }
}
async function restartClient(context, notify) {
    try {
        await stopClient();
        await startClient(context);
        if (notify) {
            vscode.window.showInformationMessage('BCL language server restarted.');
        }
    }
    catch (error) {
        client = undefined;
        reportError('BCL language server restart failed', error);
    }
}
function resolveServerCommand(context) {
    const configured = vscode.workspace.getConfiguration('bcl').get('languageServer.path') || '';
    if (configured) {
        return { command: configured };
    }
    const bundled = bundledServerPath(context);
    if (bundled && fs.existsSync(bundled)) {
        return { command: bundled };
    }
    const root = workspacePath();
    return { command: 'go', args: ['run', './cmd/bcl-lsp'], cwd: root };
}
function bundledServerPath(context) {
    const platform = `${process.platform}-${process.arch}`;
    const exe = process.platform === 'win32' ? 'bcl-lsp.exe' : 'bcl-lsp';
    return context.asAbsolutePath(path.join('bin', platform, exe));
}
function runCurrentFileCommand(command) {
    const editor = vscode.window.activeTextEditor;
    if (!editor || editor.document.languageId !== LANGUAGE_ID) {
        vscode.window.showWarningMessage('Open a BCL file first.');
        return;
    }
    runBclCommand([command, editor.document.uri.fsPath]);
}
function runBclCommand(args) {
    const cli = vscode.workspace.getConfiguration('bcl').get('cli.path') || 'bcl';
    const terminal = vscode.window.createTerminal({ name: 'BCL' });
    terminal.show();
    terminal.sendText([shellQuote(cli), ...args.map(shellQuote)].join(' '));
}
async function runRouteCoverage() {
    const definition = await promptDefinitionName();
    if (!definition) {
        return;
    }
    try {
        const result = await conditionRequest('GET', `/v1/definitions/${encodeURIComponent(definition)}/route-coverage`);
        await showJsonDocument(`route-coverage-${definition}.json`, result);
    }
    catch (error) {
        reportError('Condition route coverage failed', error);
    }
}
async function runLifecyclePlayground() {
    const definition = await promptDefinitionName();
    if (!definition) {
        return;
    }
    const lifecycle = await promptInput('Lifecycle ID', vscode.workspace.getConfiguration('bcl.condition').get('defaultLifecycle') || 'http_request');
    if (!lifecycle) {
        return;
    }
    const phase = await promptPick('Phase', ['pre', 'post', 'error', 'finally'], 'post');
    if (!phase) {
        return;
    }
    const method = await promptPick('HTTP method', ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'], 'GET');
    if (!method) {
        return;
    }
    const requestPath = await promptInput('Request path', '/endpoint-error');
    if (!requestPath) {
        return;
    }
    const statusValue = phase === 'pre' ? '' : await promptInput('Response status for post/error phases', '500');
    const body = {
        phase,
        method,
        path: requestPath,
        request: {
            headers: {
                content_type: 'application/json',
                x_request_id: `vscode-${Date.now()}`
            },
            body: {
                source: 'vscode-lifecycle-playground'
            },
            format: 'json'
        },
        input: { request: { actor_key: requestPath, application_key: definition } },
        dry_run: true
    };
    if (statusValue) {
        body.response = {
            status: Number(statusValue) || statusValue,
            headers: { content_type: 'application/json' },
            body: { source: 'vscode-lifecycle-playground' },
            format: 'json'
        };
    }
    try {
        const result = await conditionRequest('POST', `/v1/definitions/${encodeURIComponent(definition)}/lifecycles/${encodeURIComponent(lifecycle)}/evaluate`, body);
        await showJsonDocument(`lifecycle-${definition}-${phase}.json`, result);
    }
    catch (error) {
        reportError('Condition lifecycle playground failed', error);
    }
}
async function runStateCompaction() {
    const before = await promptInput('Compact records before RFC3339 timestamp', new Date().toISOString());
    if (!before) {
        return;
    }
    const definition = await promptInput('Definition filter (optional)', inferredDefinitionName());
    try {
        const result = await conditionRequest('POST', '/v1/state/compact', {
            before,
            definition: definition || undefined
        });
        await showJsonDocument('condition-state-compaction.json', result);
    }
    catch (error) {
        reportError('Condition state compaction failed', error);
    }
}
async function openWorkspaceFile(relativePath) {
    const uri = vscode.Uri.file(path.join(workspacePath(), relativePath));
    const document = await vscode.workspace.openTextDocument(uri);
    await vscode.window.showTextDocument(document);
}
async function conditionRequest(method, pathPart, body) {
    const cfg = conditionConfig();
    const base = (cfg.get('url') || 'http://127.0.0.1:8080').replace(/\/+$/, '');
    const tenant = cfg.get('tenant') || 'default';
    const url = new URL(pathPart, `${base}/`);
    const payload = body === undefined ? undefined : Buffer.from(JSON.stringify(body));
    const transport = url.protocol === 'https:' ? https : http;
    return new Promise((resolve, reject) => {
        const req = transport.request(url, {
            method,
            headers: {
                'Content-Type': 'application/json',
                'X-Tenant-ID': tenant,
                'X-Roles': 'condition-admin',
                ...(payload ? { 'Content-Length': String(payload.length) } : {})
            }
        }, (res) => {
            const chunks = [];
            res.on('data', (chunk) => chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)));
            res.on('end', () => {
                const text = Buffer.concat(chunks).toString('utf8');
                if ((res.statusCode || 0) >= 400) {
                    reject(new Error(`${res.statusCode}: ${text}`));
                    return;
                }
                try {
                    resolve(text ? JSON.parse(text) : {});
                }
                catch {
                    resolve(text);
                }
            });
        });
        req.on('error', reject);
        if (payload) {
            req.write(payload);
        }
        req.end();
    });
}
async function showJsonDocument(name, value) {
    const doc = await vscode.workspace.openTextDocument({
        language: 'json',
        content: JSON.stringify(value, null, 2)
    });
    await vscode.window.showTextDocument(doc, { preview: false });
    output(`Opened ${name}`);
}
async function promptDefinitionName() {
    return promptInput('Definition name', inferredDefinitionName());
}
function inferredDefinitionName() {
    const text = vscode.window.activeTextEditor?.document.getText() || '';
    const moduleMatch = text.match(/\bmodule\s+"([^"]+)"/);
    if (moduleMatch?.[1]) {
        return moduleMatch[1];
    }
    const file = vscode.window.activeTextEditor?.document.fileName || '';
    return path.basename(path.dirname(file)) || 'request-lifecycle';
}
function conditionConfig() {
    return vscode.workspace.getConfiguration('bcl.conditionServer');
}
function promptInput(title, value) {
    return vscode.window.showInputBox({ title, value });
}
async function promptPick(title, values, fallback) {
    return vscode.window.showQuickPick(values, { title, placeHolder: fallback });
}
async function provideRichHover(document, position) {
    if (!client) {
        return undefined;
    }
    try {
        const detail = await client.sendRequest('bcl/hoverDetail', {
            textDocument: { uri: document.uri.toString() },
            position: { line: position.line, character: position.character }
        });
        if (!detail?.contents) {
            return undefined;
        }
        const markdown = new vscode.MarkdownString(detail.contents, true);
        markdown.isTrusted = {
            enabledCommands: TRUSTED_COMMANDS
        };
        markdown.supportHtml = false;
        return new vscode.Hover(markdown, detail.range);
    }
    catch (error) {
        output(`BCL rich hover failed: ${errorMessage(error)}`);
        return undefined;
    }
}
function workspacePath() {
    return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || process.cwd();
}
function shellQuote(value) {
    if (/^[A-Za-z0-9_./:=+-]+$/.test(value)) {
        return value;
    }
    return `'${value.replace(/'/g, `'\\''`)}'`;
}
function reportError(prefix, error) {
    const message = `${prefix}: ${errorMessage(error)}`;
    output(message);
    vscode.window.showErrorMessage(message);
}
function output(message) {
    outputChannel?.appendLine(message);
}
function errorMessage(error) {
    if (error instanceof Error) {
        return error.message;
    }
    return String(error);
}
//# sourceMappingURL=extension.js.map