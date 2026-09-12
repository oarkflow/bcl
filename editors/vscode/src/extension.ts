import * as fs from 'fs';
import * as http from 'http';
import * as https from 'https';
import * as path from 'path';
import * as vscode from 'vscode';
import { CloseAction, ErrorAction, LanguageClient, LanguageClientOptions, ServerOptions, TransportKind } from 'vscode-languageclient/node';

let client: LanguageClient | undefined;
let outputChannel: vscode.OutputChannel | undefined;
let serverCrashes = 0;
let extensionContext: vscode.ExtensionContext | undefined;

const MAX_SERVER_RESTARTS = 3;

const LANGUAGE_ID = 'bcl';
const WATCHED_FILE_GLOBS = ['**/*.bcl', '**/*.schema'];
const TRUSTED_COMMANDS = [
  'bcl.compileCurrentFile',
  'bcl.explainCurrentFile',
  'bcl.restartLanguageServer'
];

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  extensionContext = context;
  outputChannel = vscode.window.createOutputChannel('BCL Language Server');
  context.subscriptions.push(
    outputChannel,
    vscode.commands.registerCommand('bcl.restartLanguageServer', async () => {
      await restartClient(context, true);
    }),
    vscode.commands.registerCommand('bcl.showRecentSymbols', async () => {
      if (!client) {
        vscode.window.showWarningMessage('BCL language server is not running.');
        return;
      }
      try {
        const items = await client.sendRequest<Array<{ label: string }>>('bcl/recentSymbols');
        const picked = await vscode.window.showQuickPick(items, { placeHolder: 'Recent BCL symbols' });
        if (picked) {
          await vscode.commands.executeCommand('workbench.action.quickOpen', `#${picked.label}`);
        }
      } catch (error) {
        reportError('BCL recent symbols failed', error);
      }
    }),
    vscode.commands.registerCommand('bcl.showLanguageServerLog', () => outputChannel?.show(true)),
    vscode.commands.registerCommand('bcl.formatFile', (resource?: vscode.Uri) => formatFile(resource)),
    vscode.commands.registerCommand('bcl.formatSection', () => formatSection()),
    vscode.commands.registerCommand('bcl.formatFolder', (resource?: vscode.Uri) => formatFolder(resource)),
    vscode.commands.registerCommand('bcl.validateWorkspace', () => runBclCommand(['validate', '--strict', workspacePath()])),
    vscode.commands.registerCommand('bcl.compileCurrentFile', () => runCurrentFileCommand('compile')),
    vscode.commands.registerCommand('bcl.explainCurrentFile', () => runCurrentFileCommand('explain')),
    vscode.commands.registerCommand('bcl.condition.routeCoverage', () => runRouteCoverage()),
    vscode.commands.registerCommand('bcl.condition.lifecyclePlayground', () => runLifecyclePlayground()),
    vscode.commands.registerCommand('bcl.condition.compactState', () => runStateCompaction()),
    vscode.languages.registerHoverProvider({ language: LANGUAGE_ID, scheme: 'file' }, {
      provideHover: async (document, position) => provideRichHover(document, position)
    })
  );

  await restartClient(context, false);
}

export async function deactivate(): Promise<void> {
  await stopClient();
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
  const command = resolveServerCommand(context);
  if (!command.command) {
    await reportServerUnavailable('No BCL language server could be found.', command);
    return;
  }
  output(`Starting BCL language server: ${command.command}${command.args ? ` ${command.args.join(' ')}` : ''} (${command.source})`);
  const serverOptions: ServerOptions = command.args
    ? { command: command.command, args: command.args, transport: TransportKind.stdio, options: { cwd: command.cwd } }
    : { command: command.command, transport: TransportKind.stdio };

  const clientOptions: LanguageClientOptions = {
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
    traceOutputChannel: outputChannel,
    // Without this, a server that dies is retried silently until the client gives
    // up with "Cannot call write after a stream was destroyed" and no explanation.
    errorHandler: {
      error: (error, message, count) => {
        output(`Language server error (${count ?? 0}): ${errorMessage(error)}${message ? ` while handling ${message.jsonrpc}` : ''}`);
        return { action: (count ?? 0) < 3 ? ErrorAction.Continue : ErrorAction.Shutdown };
      },
      closed: () => {
        serverCrashes++;
        if (serverCrashes <= MAX_SERVER_RESTARTS) {
          output(`Language server exited unexpectedly (${serverCrashes}/${MAX_SERVER_RESTARTS}); restarting.`);
          return { action: CloseAction.Restart };
        }
        void reportServerUnavailable(
          `The BCL language server (${command.command}) keeps exiting.`,
          command
        );
        return { action: CloseAction.DoNotRestart, handled: true };
      }
    }
  };

  client = new LanguageClient('bcl', 'BCL Language Server', serverOptions, clientOptions);
  await client.start();
  serverCrashes = 0;
  output('BCL language server is ready.');
}

/**
 * Explains an unusable server once, with the two things that actually fix it.
 * A silent dead server is the worst outcome: every feature stops with no reason.
 */
async function reportServerUnavailable(reason: string, command?: ServerCommand): Promise<void> {
  const detail = command?.source === 'go-run'
    ? 'It was started with "go run", which needs the Go toolchain on VS Code\'s PATH and the BCL repository as the open folder.'
    : `No prebuilt server was found for ${process.platform}-${process.arch}.`;
  output(`${reason} ${detail}`);
  const choice = await vscode.window.showErrorMessage(
    `${reason} ${detail}`,
    'Show Log',
    'How to fix'
  );
  if (choice === 'Show Log') {
    outputChannel?.show(true);
  } else if (choice === 'How to fix') {
    void vscode.window.showInformationMessage(
      'Build the server with "make vscode-extension-lsp" in the BCL repository, or set "bcl.languageServer.path" to an existing bcl-lsp binary.',
      { modal: true }
    );
  }
}

async function stopClient(): Promise<void> {
  if (client) {
    const old = client;
    client = undefined;
    try {
      await old.stop();
    } catch (error) {
      output(`Ignoring BCL language server stop error: ${errorMessage(error)}`);
    }
  }
}

async function restartClient(context: vscode.ExtensionContext, notify: boolean): Promise<void> {
  try {
    await stopClient();
    await startClient(context);
    if (notify) {
      vscode.window.showInformationMessage('BCL language server restarted.');
    }
  } catch (error) {
    client = undefined;
    reportError('BCL language server restart failed', error);
  }
}

interface ServerCommand {
  command: string;
  args?: string[];
  cwd?: string;
  source: 'setting' | 'bundled' | 'path' | 'go-run';
}

/**
 * Picks the language server to run, most specific first, and only falls back to
 * "go run" when the open folder really is a BCL checkout with a Go toolchain
 * available. Choosing "go run" blindly is how the server ends up failing to
 * spawn at all, which surfaces as an unexplained connection error.
 */
function resolveServerCommand(context: vscode.ExtensionContext): ServerCommand {
  const configured = vscode.workspace.getConfiguration('bcl').get<string>('languageServer.path') || '';
  if (configured) {
    if (!isExecutableFile(configured)) {
      output(`bcl.languageServer.path is set to "${configured}", which is not an executable file.`);
    }
    return { command: configured, source: 'setting' };
  }

  for (const candidate of bundledServerPaths(context)) {
    if (isExecutableFile(candidate)) {
      return { command: candidate, source: 'bundled' };
    }
  }

  const onPath = findOnPath(process.platform === 'win32' ? 'bcl-lsp.exe' : 'bcl-lsp');
  if (onPath) {
    return { command: onPath, source: 'path' };
  }

  const root = workspacePath();
  if (fs.existsSync(path.join(root, 'cmd', 'bcl-lsp'))) {
    const go = findOnPath(process.platform === 'win32' ? 'go.exe' : 'go');
    if (go) {
      return { command: go, args: ['run', './cmd/bcl-lsp'], cwd: root, source: 'go-run' };
    }
    output('Found ./cmd/bcl-lsp but no "go" on PATH; cannot build the language server on the fly.');
  }
  return { command: '', source: 'go-run' };
}

/**
 * Candidate binaries for this machine. The arch alias covers an Intel-VS Code
 * (or Rosetta) process on an Apple Silicon host, where process.arch reports x64
 * while the checked-in binary is arm64 - a mismatch that silently disables the
 * whole extension.
 */
function bundledServerPaths(context: vscode.ExtensionContext): string[] {
  return bundledBinaryPaths(context, process.platform === 'win32' ? 'bcl-lsp.exe' : 'bcl-lsp');
}

function bundledBinaryPaths(context: vscode.ExtensionContext, exe: string): string[] {
  const archAliases: Record<string, string[]> = {
    arm64: ['arm64', 'x64'],
    x64: ['x64', 'arm64']
  };
  const arches = archAliases[process.arch] ?? [process.arch];
  const candidates = arches.map((arch) => path.join('bin', `${process.platform}-${arch}`, exe));
  candidates.push(path.join('bin', exe));
  return candidates.map((relative) => context.asAbsolutePath(relative));
}

function isExecutableFile(candidate: string): boolean {
  try {
    if (!fs.statSync(candidate).isFile()) {
      return false;
    }
    fs.accessSync(candidate, fs.constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function findOnPath(binary: string): string | undefined {
  const entries = (process.env.PATH || '').split(path.delimiter);
  // VS Code on macOS is often launched from Finder with a minimal PATH, so the
  // usual Go install locations are checked explicitly.
  if (process.platform !== 'win32') {
    entries.push('/usr/local/go/bin', '/opt/homebrew/bin', '/usr/local/bin', path.join(process.env.HOME || '', 'go', 'bin'));
  }
  for (const entry of entries) {
    if (!entry) {
      continue;
    }
    const candidate = path.join(entry, binary);
    if (isExecutableFile(candidate)) {
      return candidate;
    }
  }
  return undefined;
}

/** Formats one BCL file: the given resource, or the active editor's document. */
async function formatFile(resource?: vscode.Uri): Promise<void> {
  const uri = resource ?? vscode.window.activeTextEditor?.document.uri;
  if (!uri) {
    vscode.window.showWarningMessage('Open a BCL file first.');
    return;
  }
  try {
    const edited = await formatUri(uri);
    if (!edited) {
      vscode.window.showInformationMessage(`${path.basename(uri.fsPath)} is already formatted.`);
    }
  } catch (error) {
    reportError('BCL format file failed', error);
  }
}

/**
 * Formats the block the cursor sits in - the innermost `node`, `page`, `form`,
 * `workflow`, ... declaration - leaving the rest of the file untouched. The
 * enclosing range comes from the language server's own document symbols, so a
 * "section" is always a real BCL declaration rather than a guess at indentation.
 */
async function formatSection(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.document.languageId !== LANGUAGE_ID) {
    vscode.window.showWarningMessage('Open a BCL file first.');
    return;
  }
  const document = editor.document;
  try {
    const selection = editor.selection;
    let range: vscode.Range | undefined;
    if (!selection.isEmpty) {
      range = new vscode.Range(selection.start.line, 0, selection.end.line, document.lineAt(selection.end.line).text.length);
    } else {
      range = await enclosingSymbolRange(document, selection.active);
    }
    if (!range) {
      vscode.window.showInformationMessage('No enclosing BCL block found at the cursor; use Format File instead.');
      return;
    }
    const edits = await vscode.commands.executeCommand<vscode.TextEdit[]>(
      'vscode.executeFormatRangeProvider',
      document.uri,
      range,
      formattingOptionsFor(document)
    );
    if (!edits || edits.length === 0) {
      vscode.window.showInformationMessage('This section is already formatted.');
      return;
    }
    const workspaceEdit = new vscode.WorkspaceEdit();
    workspaceEdit.set(document.uri, edits);
    await vscode.workspace.applyEdit(workspaceEdit);
  } catch (error) {
    reportError('BCL format section failed', error);
  }
}

/** Formats every BCL file under a folder, reporting how many changed. */
async function formatFolder(resource?: vscode.Uri): Promise<void> {
  const folder = resource ?? (await pickFolder());
  if (!folder) {
    return;
  }
  const pattern = new vscode.RelativePattern(folder, '**/*.{bcl,schema}');
  const files = await vscode.workspace.findFiles(pattern, '**/{node_modules,.git,out,dist,vendor}/**');
  if (files.length === 0) {
    vscode.window.showInformationMessage(`No BCL files found under ${path.basename(folder.fsPath)}.`);
    return;
  }
  await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: `Formatting ${files.length} BCL file(s)`, cancellable: true },
    async (progress, token) => {
      let changed = 0;
      let failed = 0;
      for (const [index, file] of files.entries()) {
        if (token.isCancellationRequested) {
          break;
        }
        progress.report({ message: path.basename(file.fsPath), increment: 100 / files.length });
        try {
          if (await formatUri(file)) {
            changed++;
          }
        } catch (error) {
          failed++;
          output(`Format failed for ${file.fsPath}: ${errorMessage(error)}`);
        }
        void index;
      }
      const summary = `Formatted ${changed} of ${files.length} BCL file(s)${failed ? `, ${failed} failed` : ''}.`;
      if (failed) {
        vscode.window.showWarningMessage(`${summary} See the BCL Language Server output for details.`);
      } else {
        vscode.window.showInformationMessage(summary);
      }
    }
  );
}

/** Formats one document through the language server and saves it. Returns true when it changed. */
async function formatUri(uri: vscode.Uri): Promise<boolean> {
  const document = await vscode.workspace.openTextDocument(uri);
  const edits = await vscode.commands.executeCommand<vscode.TextEdit[]>(
    'vscode.executeFormatDocumentProvider',
    uri,
    formattingOptionsFor(document)
  );
  if (!edits || edits.length === 0) {
    return false;
  }
  const workspaceEdit = new vscode.WorkspaceEdit();
  workspaceEdit.set(uri, edits);
  if (!(await vscode.workspace.applyEdit(workspaceEdit))) {
    return false;
  }
  if (document.isDirty) {
    await document.save();
  }
  return true;
}

async function enclosingSymbolRange(document: vscode.TextDocument, at: vscode.Position): Promise<vscode.Range | undefined> {
  const symbols = await vscode.commands.executeCommand<vscode.DocumentSymbol[] | vscode.SymbolInformation[]>(
    'vscode.executeDocumentSymbolProvider',
    document.uri
  );
  let best: vscode.Range | undefined;
  const visit = (items: Array<vscode.DocumentSymbol | vscode.SymbolInformation>): void => {
    for (const item of items) {
      const range = 'range' in item ? item.range : item.location.range;
      if (!range.contains(at)) {
        continue;
      }
      if (!best || range.start.isAfterOrEqual(best.start)) {
        best = range;
      }
      const children = (item as vscode.DocumentSymbol).children;
      if (children && children.length > 0) {
        visit(children);
      }
    }
  };
  visit(symbols ?? []);
  return best;
}

function formattingOptionsFor(document: vscode.TextDocument): vscode.FormattingOptions {
  const config = vscode.workspace.getConfiguration('editor', document.uri);
  return {
    tabSize: config.get<number>('tabSize') ?? 2,
    insertSpaces: config.get<boolean>('insertSpaces') ?? true
  };
}

async function pickFolder(): Promise<vscode.Uri | undefined> {
  const folders = vscode.workspace.workspaceFolders ?? [];
  if (folders.length === 1) {
    return folders[0].uri;
  }
  if (folders.length > 1) {
    const picked = await vscode.window.showQuickPick(
      folders.map((folder) => ({ label: folder.name, description: folder.uri.fsPath, uri: folder.uri })),
      { placeHolder: 'Format BCL files in which folder?' }
    );
    return picked?.uri;
  }
  const chosen = await vscode.window.showOpenDialog({ canSelectFolders: true, canSelectFiles: false, canSelectMany: false });
  return chosen?.[0];
}

function runCurrentFileCommand(command: string): void {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.document.languageId !== LANGUAGE_ID) {
    vscode.window.showWarningMessage('Open a BCL file first.');
    return;
  }
  runBclCommand([command, editor.document.uri.fsPath]);
}

function runBclCommand(args: string[]): void {
  const cli = resolveCli();
  if (!cli) {
    void vscode.window.showErrorMessage(
      'No BCL CLI found. Set "bcl.cli.path", or build one with "make vscode-extension-lsp" in the BCL repository.'
    );
    return;
  }
  const terminal = vscode.window.createTerminal({ name: 'BCL' });
  terminal.show();
  terminal.sendText([shellQuote(cli), ...args.map(shellQuote)].join(' '));
}

/**
 * Finds the bcl CLI: the setting, then the binary bundled with the extension,
 * then PATH. Resolving the bundled copy is what lets compile/validate/explain
 * work in a window that is not a BCL checkout.
 */
function resolveCli(): string | undefined {
  const configured = vscode.workspace.getConfiguration('bcl').get<string>('cli.path') || '';
  if (configured && configured !== 'bcl') {
    return configured;
  }
  if (extensionContext) {
    const exe = process.platform === 'win32' ? 'bcl.exe' : 'bcl';
    for (const candidate of bundledBinaryPaths(extensionContext, exe)) {
      if (isExecutableFile(candidate)) {
        return candidate;
      }
    }
  }
  return findOnPath(process.platform === 'win32' ? 'bcl.exe' : 'bcl') ?? (configured || undefined);
}

async function runRouteCoverage(): Promise<void> {
  const definition = await promptDefinitionName();
  if (!definition) {
    return;
  }
  try {
    const result = await conditionRequest('GET', `/v1/definitions/${encodeURIComponent(definition)}/route-coverage`);
    await showJsonDocument(`route-coverage-${definition}.json`, result);
  } catch (error) {
    reportError('Condition route coverage failed', error);
  }
}

async function runLifecyclePlayground(): Promise<void> {
  const definition = await promptDefinitionName();
  if (!definition) {
    return;
  }
  const lifecycle = await promptInput('Lifecycle ID', vscode.workspace.getConfiguration('bcl.condition').get<string>('defaultLifecycle') || 'http_request');
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
  const body: Record<string, unknown> = {
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
  } catch (error) {
    reportError('Condition lifecycle playground failed', error);
  }
}

async function runStateCompaction(): Promise<void> {
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
  } catch (error) {
    reportError('Condition state compaction failed', error);
  }
}

async function conditionRequest(method: string, pathPart: string, body?: unknown): Promise<unknown> {
  const cfg = conditionConfig();
  const base = (cfg.get<string>('url') || 'http://127.0.0.1:8080').replace(/\/+$/, '');
  const tenant = cfg.get<string>('tenant') || 'default';
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
      const chunks: Buffer[] = [];
      res.on('data', (chunk) => chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)));
      res.on('end', () => {
        const text = Buffer.concat(chunks).toString('utf8');
        if ((res.statusCode || 0) >= 400) {
          reject(new Error(`${res.statusCode}: ${text}`));
          return;
        }
        try {
          resolve(text ? JSON.parse(text) : {});
        } catch {
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

async function showJsonDocument(name: string, value: unknown): Promise<void> {
  const doc = await vscode.workspace.openTextDocument({
    language: 'json',
    content: JSON.stringify(value, null, 2)
  });
  await vscode.window.showTextDocument(doc, { preview: false });
  output(`Opened ${name}`);
}

async function promptDefinitionName(): Promise<string | undefined> {
  return promptInput('Definition name', inferredDefinitionName());
}

function inferredDefinitionName(): string {
  const text = vscode.window.activeTextEditor?.document.getText() || '';
  const moduleMatch = text.match(/\bmodule\s+"([^"]+)"/);
  if (moduleMatch?.[1]) {
    return moduleMatch[1];
  }
  const file = vscode.window.activeTextEditor?.document.fileName || '';
  return path.basename(path.dirname(file)) || 'request-lifecycle';
}

function conditionConfig(): vscode.WorkspaceConfiguration {
  return vscode.workspace.getConfiguration('bcl.conditionServer');
}

function promptInput(title: string, value: string): Thenable<string | undefined> {
  return vscode.window.showInputBox({ title, value });
}

async function promptPick(title: string, values: string[], fallback: string): Promise<string | undefined> {
  return vscode.window.showQuickPick(values, { title, placeHolder: fallback });
}

async function provideRichHover(document: vscode.TextDocument, position: vscode.Position): Promise<vscode.Hover | undefined> {
  if (!client) {
    return undefined;
  }
  try {
    const detail = await client.sendRequest<{ contents: string; range?: vscode.Range } | null>('bcl/hoverDetail', {
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
  } catch (error) {
    output(`BCL rich hover failed: ${errorMessage(error)}`);
    return undefined;
  }
}

function workspacePath(): string {
  return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || process.cwd();
}

function shellQuote(value: string): string {
  if (/^[A-Za-z0-9_./:=+-]+$/.test(value)) {
    return value;
  }
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

function reportError(prefix: string, error: unknown): void {
  const message = `${prefix}: ${errorMessage(error)}`;
  output(message);
  vscode.window.showErrorMessage(message);
}

function output(message: string): void {
  outputChannel?.appendLine(message);
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}
