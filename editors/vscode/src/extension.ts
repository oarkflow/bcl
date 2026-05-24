import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import { LanguageClient, LanguageClientOptions, ServerOptions, TransportKind } from 'vscode-languageclient/node';

let client: LanguageClient | undefined;
let outputChannel: vscode.OutputChannel | undefined;

const LANGUAGE_ID = 'bcl';
const WATCHED_FILE_GLOBS = ['**/*.bcl', '**/*.schema'];
const TRUSTED_COMMANDS = [
  'bcl.compileCurrentFile',
  'bcl.explainCurrentFile',
  'bcl.restartLanguageServer'
];

export async function activate(context: vscode.ExtensionContext): Promise<void> {
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
    vscode.commands.registerCommand('bcl.validateWorkspace', () => runBclCommand(['validate', '--strict', workspacePath()])),
    vscode.commands.registerCommand('bcl.compileCurrentFile', () => runCurrentFileCommand('compile')),
    vscode.commands.registerCommand('bcl.explainCurrentFile', () => runCurrentFileCommand('explain')),
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
  output(`Starting BCL language server: ${command.command}${command.args ? ` ${command.args.join(' ')}` : ''}`);
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
    traceOutputChannel: outputChannel
  };

  client = new LanguageClient('bcl', 'BCL Language Server', serverOptions, clientOptions);
  await client.start();
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

function resolveServerCommand(context: vscode.ExtensionContext): { command: string; args?: string[]; cwd?: string } {
  const configured = vscode.workspace.getConfiguration('bcl').get<string>('languageServer.path') || '';
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

function bundledServerPath(context: vscode.ExtensionContext): string | undefined {
  const platform = `${process.platform}-${process.arch}`;
  const exe = process.platform === 'win32' ? 'bcl-lsp.exe' : 'bcl-lsp';
  return context.asAbsolutePath(path.join('bin', platform, exe));
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
  const cli = vscode.workspace.getConfiguration('bcl').get<string>('cli.path') || 'bcl';
  const terminal = vscode.window.createTerminal({ name: 'BCL' });
  terminal.show();
  terminal.sendText([shellQuote(cli), ...args.map(shellQuote)].join(' '));
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
