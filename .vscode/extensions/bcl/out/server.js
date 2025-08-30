"use strict";
/* --------------------------------------------------------------------------------------------
 * Copyright (c) Microsoft Corporation. All rights reserved.
 * Licensed under the MIT License. See License.txt in the project root for license information.
 * ------------------------------------------------------------------------------------------ */
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
const node_1 = require("vscode-languageserver/node");
const vscode_languageserver_textdocument_1 = require("vscode-languageserver-textdocument");
const path = __importStar(require("path"));
const fs = __importStar(require("fs"));
// Create a connection for the server, using Node's IPC as the transport.
// Also include all preview / proposed LSP features.
const connection = (0, node_1.createConnection)();
// Create a simple text document manager.
const documents = new node_1.TextDocuments(vscode_languageserver_textdocument_1.TextDocument);
const symbolTable = new Map();
const includeTable = new Map();
let hasConfigurationCapability = false;
let hasWorkspaceFolderCapability = false;
let hasDiagnosticRelatedInformationCapability = false;
connection.onInitialize((params) => {
    const capabilities = params.capabilities;
    // Does the client support the `workspace/configuration` request?
    hasConfigurationCapability = !!(capabilities.workspace && !!capabilities.workspace.configuration);
    hasWorkspaceFolderCapability = !!(capabilities.workspace && !!capabilities.workspace.workspaceFolders);
    hasDiagnosticRelatedInformationCapability = !!(capabilities.textDocument &&
        capabilities.textDocument.publishDiagnostics &&
        capabilities.textDocument.publishDiagnostics.relatedInformation);
    const result = {
        capabilities: {
            textDocumentSync: node_1.TextDocumentSyncKind.Incremental,
            // Tell the client that this server supports code completion.
            completionProvider: {
                resolveProvider: true
            },
            hoverProvider: true,
            definitionProvider: true,
            referencesProvider: true
        }
    };
    if (hasWorkspaceFolderCapability) {
        result.capabilities.workspace = {
            workspaceFolders: {
                supported: true
            }
        };
    }
    return result;
});
connection.onInitialized(() => {
    if (hasConfigurationCapability) {
        // Register for all configuration changes.
        connection.client.register(node_1.DidChangeConfigurationNotification.type, undefined);
    }
    if (hasWorkspaceFolderCapability) {
        connection.workspace.onDidChangeWorkspaceFolders((_event) => {
            connection.console.log('Workspace folder change event received.');
        });
    }
});
// The global settings, used when the `workspace/configuration` request is not supported by the client.
// Please note that this is not the case when using this server with the client provided in this example
// but could happen with other clients.
const defaultSettings = { maxNumberOfProblems: 1000 };
let globalSettings = defaultSettings;
// Cache the settings of all open documents
const documentSettings = new Map();
connection.onDidChangeConfiguration((change) => {
    if (hasConfigurationCapability) {
        // Reset all cached document settings
        documentSettings.clear();
    }
    else {
        globalSettings = ((change.settings.bcl || defaultSettings));
    }
    // Revalidate all open text documents
    documents.all().forEach(validateTextDocument);
});
function getDocumentSettings(resource) {
    if (!hasConfigurationCapability) {
        return Promise.resolve(globalSettings);
    }
    let result = documentSettings.get(resource);
    if (!result) {
        result = connection.workspace.getConfiguration({
            scopeUri: resource,
            section: 'bcl'
        });
        documentSettings.set(resource, result);
    }
    return result;
}
// Only keep settings for open documents
documents.onDidClose((e) => {
    documentSettings.delete(e.document.uri);
});
// The content of a text document has changed. This event is emitted
// when the text document first opened or when its content has changed.
documents.onDidChangeContent((change) => {
    validateTextDocument(change.document);
});
// Enhanced BCL validation with syntax error checking
async function validateTextDocument(textDocument) {
    // In this simple example we get the settings for every validate run.
    const settings = await getDocumentSettings(textDocument.uri);
    // Clear symbol table for this document
    symbolTable.set(textDocument.uri, []);
    includeTable.set(textDocument.uri, []);
    // The validator creates diagnostics for BCL syntax errors
    const text = textDocument.getText();
    let problems = 0;
    const diagnostics = [];
    // Check for unmatched braces
    const braceStack = [];
    const lines = text.split('\n');
    for (let lineIndex = 0; lineIndex < lines.length; lineIndex++) {
        const line = lines[lineIndex];
        for (let charIndex = 0; charIndex < line.length; charIndex++) {
            const char = line[charIndex];
            const position = node_1.Position.create(lineIndex, charIndex);
            if (char === '{' || char === '[' || char === '(') {
                braceStack.push({ char, position });
            }
            else if (char === '}' || char === ']' || char === ')') {
                if (braceStack.length === 0) {
                    // Unmatched closing brace
                    problems++;
                    const diagnostic = {
                        severity: node_1.DiagnosticSeverity.Error,
                        range: {
                            start: position,
                            end: node_1.Position.create(lineIndex, charIndex + 1)
                        },
                        message: `Unmatched closing brace '${char}'`,
                        source: 'bcl'
                    };
                    diagnostics.push(diagnostic);
                }
                else {
                    const last = braceStack.pop();
                    if ((char === '}' && last.char !== '{') ||
                        (char === ']' && last.char !== '[') ||
                        (char === ')' && last.char !== '(')) {
                        // Mismatched brace types
                        problems++;
                        const diagnostic = {
                            severity: node_1.DiagnosticSeverity.Error,
                            range: {
                                start: last.position,
                                end: node_1.Position.create(last.position.line, last.position.character + 1)
                            },
                            message: `Mismatched braces: expected '${getMatchingBrace(last.char)}' but found '${char}'`,
                            source: 'bcl'
                        };
                        diagnostics.push(diagnostic);
                    }
                }
            }
        }
    }
    // Check for unclosed braces
    while (braceStack.length > 0 && problems < settings.maxNumberOfProblems) {
        const unclosed = braceStack.pop();
        problems++;
        const diagnostic = {
            severity: node_1.DiagnosticSeverity.Warning,
            range: {
                start: unclosed.position,
                end: node_1.Position.create(unclosed.position.line, unclosed.position.character + 1)
            },
            message: `Unclosed brace '${unclosed.char}'`,
            source: 'bcl'
        };
        diagnostics.push(diagnostic);
    }
    // Check for common BCL syntax issues and populate symbol table
    // Pattern for variable assignments: identifier = value
    const assignmentPattern = /^\s*([a-zA-Z_][a-zA-Z0-9_.-]*)\s*=\s*(.*)$/gm;
    let match;
    while ((match = assignmentPattern.exec(text)) && problems < settings.maxNumberOfProblems) {
        const variableName = match[1];
        const startPos = textDocument.positionAt(match.index);
        const endPos = textDocument.positionAt(match.index + match[0].length);
        // Add to symbol table
        const symbols = symbolTable.get(textDocument.uri) || [];
        symbols.push({
            name: variableName,
            location: node_1.Location.create(textDocument.uri, node_1.Range.create(startPos, endPos)),
            type: "variable"
        });
        symbolTable.set(textDocument.uri, symbols);
        // For dot notation, also add individual components to support navigation
        if (variableName.includes('.')) {
            const parts = variableName.split('.');
            // Add each component as a separate symbol for navigation
            for (let i = 1; i < parts.length; i++) {
                // For tunnel.myservice-prod2.mapping.user_id, we want to add myservice-prod2 as a symbol
                const componentName = parts[i];
                // Only add if it's not already in the symbol table
                let found = false;
                for (const symbol of symbols) {
                    if (symbol.name === componentName) {
                        found = true;
                        break;
                    }
                }
                if (!found) {
                    symbols.push({
                        name: componentName,
                        location: node_1.Location.create(textDocument.uri, node_1.Range.create(startPos, endPos)),
                        type: "component"
                    });
                }
            }
        }
    }
    // Pattern for block definitions: identifier { ... }
    // This pattern now supports quoted block names like tunnel "myservice-prod2" {
    const blockPattern = /^\s*([a-zA-Z_][a-zA-Z0-9_.]*)\s+("[^"]*"|[a-zA-Z_][a-zA-Z0-9_.-]*)\s*\{/gm;
    while ((match = blockPattern.exec(text)) && problems < settings.maxNumberOfProblems) {
        const blockType = match[1];
        let blockName = match[2];
        // Remove quotes from quoted block names
        if (blockName.startsWith('"') && blockName.endsWith('"')) {
            blockName = blockName.substring(1, blockName.length - 1);
        }
        const blockStart = match.index;
        const blockStartPos = textDocument.positionAt(blockStart);
        const blockEndPos = textDocument.positionAt(match.index + match[0].length);
        // Add to symbol table
        const symbols = symbolTable.get(textDocument.uri) || [];
        symbols.push({
            name: blockName,
            location: node_1.Location.create(textDocument.uri, node_1.Range.create(blockStartPos, blockEndPos)),
            type: "block"
        });
        symbolTable.set(textDocument.uri, symbols);
        // Also add the full path for dot notation access
        // For example, for tunnel "myservice-prod2", add tunnel.myservice-prod2
        const fullPath = `${blockType}.${blockName}`;
        symbols.push({
            name: fullPath,
            location: node_1.Location.create(textDocument.uri, node_1.Range.create(blockStartPos, blockEndPos)),
            type: "block-path"
        });
        // Parse block contents to find nested properties and blocks
        parseBlockContents(text, textDocument, symbols, blockStart, fullPath, problems, settings.maxNumberOfProblems);
    }
    // Parse include statements: @include "filename.bcl" or variable = @include "filename.bcl"
    const includePattern = /(@include\s+"([^"]+)"|=\s*@include\s+"([^"]+)")/g;
    let includeMatch;
    while ((includeMatch = includePattern.exec(text)) && problems < settings.maxNumberOfProblems) {
        const fullMatch = includeMatch[0];
        const fileName = includeMatch[2] || includeMatch[3]; // Get the filename from either capture group
        const matchStart = includeMatch.index;
        const matchEnd = matchStart + fullMatch.length;
        const startPos = textDocument.positionAt(matchStart);
        const endPos = textDocument.positionAt(matchEnd);
        // Resolve the file path relative to the current document
        // Convert URI to file path (handle file:// URIs)
        let documentPath = textDocument.uri;
        if (documentPath.startsWith('file://')) {
            documentPath = decodeURIComponent(documentPath.substring(7)); // Remove 'file://' prefix and decode URI
        }
        const documentDir = path.dirname(documentPath);
        const includePath = path.resolve(documentDir, fileName);
        // Add to include table
        const includes = includeTable.get(textDocument.uri) || [];
        includes.push({
            fileName: fileName,
            location: node_1.Location.create(textDocument.uri, node_1.Range.create(startPos, endPos)),
            filePath: includePath
        });
        includeTable.set(textDocument.uri, includes);
    }
    // Check for common BCL syntax issues
    const pattern = /([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*([a-zA-Z_][a-zA-Z0-9_]*\s*[+\-*/]\s*[a-zA-Z_][a-zA-Z0-9_]*)/g;
    let m;
    while ((m = pattern.exec(text)) && problems < settings.maxNumberOfProblems) {
        problems++;
        const diagnostic = {
            severity: node_1.DiagnosticSeverity.Warning,
            range: {
                start: textDocument.positionAt(m.index),
                end: textDocument.positionAt(m.index + m[0].length)
            },
            message: `Possible syntax issue: Did you mean '${m[1]} = ${m[2]}'?`,
            source: 'bcl'
        };
        diagnostics.push(diagnostic);
    }
    // Send the computed diagnostics to VSCode.
    connection.sendDiagnostics({ uri: textDocument.uri, diagnostics });
}
function getMatchingBrace(char) {
    switch (char) {
        case '{': return '}';
        case '[': return ']';
        case '(': return ')';
        default: return '';
    }
}
// Helper function to parse block contents and add nested properties to symbol table
function parseBlockContents(text, textDocument, symbols, blockStart, fullPath, problems, maxProblems) {
    // Find the matching closing brace
    let braceCount = 1;
    let contentStart = blockStart + text.substring(blockStart).indexOf('{') + 1;
    let contentEnd = contentStart;
    for (let i = contentStart; i < text.length && braceCount > 0; i++) {
        if (text[i] === '{') {
            braceCount++;
        }
        else if (text[i] === '}') {
            braceCount--;
            if (braceCount === 0) {
                contentEnd = i;
                break;
            }
        }
    }
    if (braceCount === 0) {
        // Extract block content
        const blockContent = text.substring(contentStart, contentEnd);
        // Pattern to find nested properties and blocks within the block
        // This matches: property_name = value or nested_block { ... }
        const nestedPattern = /^(\s*)([a-zA-Z_][a-zA-Z0-9_-]*)\s*(?:=|\{)/gm;
        let nestedMatch;
        while ((nestedMatch = nestedPattern.exec(blockContent)) && problems < maxProblems) {
            const propertyName = nestedMatch[2];
            const propertyStart = contentStart + nestedMatch.index + nestedMatch[1].length; // Skip leading whitespace
            const propertyEnd = propertyStart + propertyName.length;
            const propertyStartPos = textDocument.positionAt(propertyStart);
            const propertyEndPos = textDocument.positionAt(propertyEnd);
            // Add nested property to symbol table
            symbols.push({
                name: propertyName,
                location: node_1.Location.create(textDocument.uri, node_1.Range.create(propertyStartPos, propertyEndPos)),
                type: "property"
            });
            // Also add the full path for this property
            const propertyPath = `${fullPath}.${propertyName}`;
            symbols.push({
                name: propertyPath,
                location: node_1.Location.create(textDocument.uri, node_1.Range.create(propertyStartPos, propertyEndPos)),
                type: "property-path"
            });
            // If this is a nested block (ends with {), parse its contents too
            if (nestedMatch[0].trim().endsWith('{')) {
                parseBlockContents(text, textDocument, symbols, propertyStart - nestedMatch[1].length, propertyPath, problems, maxProblems);
            }
        }
    }
}
connection.onDidChangeWatchedFiles((_change) => {
    // Monitored files have change in VSCode
    connection.console.log('We received a file change event');
});
// This handler provides the initial list of the completion items.
connection.onCompletion((textDocumentPosition) => {
    // The pass parameter contains the position of the text document in
    // which code complete got requested.
    const document = documents.get(textDocumentPosition.textDocument.uri);
    if (!document) {
        return [];
    }
    const text = document.getText();
    const line = textDocumentPosition.position.line;
    const lineText = text.split('\n')[line] || '';
    // Get context-aware completions based on the current line
    const completions = [];
    // Add BCL keywords
    completions.push({
        label: 'true',
        kind: node_1.CompletionItemKind.Value,
        data: 1,
        detail: 'Boolean value',
        documentation: 'The `true` boolean value'
    }, {
        label: 'false',
        kind: node_1.CompletionItemKind.Value,
        data: 2,
        detail: 'Boolean value',
        documentation: 'The `false` boolean value'
    }, {
        label: '@include',
        kind: node_1.CompletionItemKind.Keyword,
        data: 3,
        detail: 'Include directive',
        documentation: 'Include external BCL files'
    }, {
        label: 'IF',
        kind: node_1.CompletionItemKind.Keyword,
        data: 4,
        detail: 'Control structure',
        documentation: 'Conditional IF statement'
    }, {
        label: 'ELSE',
        kind: node_1.CompletionItemKind.Keyword,
        data: 5,
        detail: 'Control structure',
        documentation: 'ELSE clause for IF statements'
    }, {
        label: 'ELSEIF',
        kind: node_1.CompletionItemKind.Keyword,
        data: 6,
        detail: 'Control structure',
        documentation: 'ELSEIF clause for IF statements'
    });
    // Add context-specific completions
    if (lineText.trim().startsWith('@')) {
        completions.push({
            label: '@exec',
            kind: node_1.CompletionItemKind.Keyword,
            data: 7,
            detail: 'Execute command',
            documentation: 'Execute external commands'
        }, {
            label: '@pipeline',
            kind: node_1.CompletionItemKind.Keyword,
            data: 8,
            detail: 'Pipeline directive',
            documentation: 'Define a pipeline of operations'
        });
    }
    // Add common BCL block types
    if (!lineText.includes('=')) {
        completions.push({
            label: 'server',
            kind: node_1.CompletionItemKind.Class,
            data: 9,
            detail: 'Server block',
            documentation: 'Define a server configuration block'
        }, {
            label: 'database',
            kind: node_1.CompletionItemKind.Class,
            data: 10,
            detail: 'Database block',
            documentation: 'Define a database configuration block'
        }, {
            label: 'tunnel',
            kind: node_1.CompletionItemKind.Class,
            data: 11,
            detail: 'Tunnel block',
            documentation: 'Define a tunnel configuration block'
        });
    }
    return completions;
});
// This handler resolves additional information for the item selected in
// the completion list.
connection.onCompletionResolve((item) => {
    // The completion items already have detail and documentation set,
    // so we don't need to do anything here for BCL.
    // This handler is kept for compatibility.
    return item;
});
connection.onHover((params) => {
    const uri = params.textDocument.uri;
    const position = params.position;
    // Get symbols for this document
    const symbols = symbolTable.get(uri) || [];
    // Find the symbol at the requested position
    for (const symbol of symbols) {
        if (isPositionInRange(position, symbol.location.range)) {
            let hoverContent = `**${symbol.name}**\n\n`;
            switch (symbol.type) {
                case "variable":
                    hoverContent += `*Variable*\n\nThis is a BCL variable assignment.`;
                    break;
                case "block":
                    hoverContent += `*Block*\n\nThis is a BCL block definition.`;
                    break;
                default:
                    hoverContent += `*${symbol.type}*\n\nThis is a BCL construct.`;
            }
            return {
                contents: {
                    kind: 'markdown',
                    value: hoverContent
                }
            };
        }
    }
    // Default hover message
    return {
        contents: {
            kind: 'markdown',
            value: 'BCL (Block Configuration Language)\n\nA lightweight configuration language.'
        }
    };
});
// Handle go to definition requests
connection.onDefinition((params) => {
    const uri = params.textDocument.uri;
    const position = params.position;
    // First check for symbol definitions
    const symbols = symbolTable.get(uri) || [];
    for (const symbol of symbols) {
        if (isPositionInRange(position, symbol.location.range)) {
            return symbol.location;
        }
    }
    // Check for dot notation components
    // For example, in tunnel.myservice-prod2.mapping.user_id,
    // if the cursor is on myservice-prod2, we want to find the tunnel "myservice-prod2" block
    const document = documents.get(uri);
    if (document) {
        const text = document.getText();
        // Find the word at the cursor position
        const offset = document.offsetAt(position);
        let start = offset;
        let end = offset;
        // Move backward to find the start of the word
        while (start > 0 && /[a-zA-Z0-9_\-]/.test(text.charAt(start - 1))) {
            start--;
        }
        // Move forward to find the end of the word
        while (end < text.length && /[a-zA-Z0-9_\-]/.test(text.charAt(end))) {
            end++;
        }
        const word = text.substring(start, end);
        // Look for symbols that match this word
        for (const symbol of symbols) {
            if (symbol.name === word) {
                return symbol.location;
            }
            // Also check if this is a component of a dot notation path
            if (symbol.name.includes('.') && symbol.name.endsWith('.' + word)) {
                return symbol.location;
            }
            // Check if this is a component at the beginning of a dot notation path
            if (symbol.name.includes('.') && symbol.name.startsWith(word + '.')) {
                return symbol.location;
            }
        }
        // Parse dot notation expressions to find the correct definition
        // Find the full dot notation expression around the cursor
        let dotStart = start;
        let dotEnd = end;
        // Move backward to find the start of the dot notation expression
        while (dotStart > 0 && /[a-zA-Z0-9_\-\.]/.test(text.charAt(dotStart - 1))) {
            dotStart--;
        }
        // Move forward to find the end of the dot notation expression
        while (dotEnd < text.length && /[a-zA-Z0-9_\-\.]/.test(text.charAt(dotEnd))) {
            dotEnd++;
        }
        const dotExpression = text.substring(dotStart, dotEnd);
        // If this is a dot notation expression, try to resolve it
        if (dotExpression.includes('.')) {
            const parts = dotExpression.split('.');
            // Find which part the cursor is on
            let currentPos = dotStart;
            let partIndex = 0;
            for (let i = 0; i < parts.length; i++) {
                const partEnd = currentPos + parts[i].length;
                if (offset >= currentPos && offset <= partEnd) {
                    partIndex = i;
                    break;
                }
                currentPos = partEnd + 1; // +1 for the dot
            }
            // Try to find the definition for this part
            if (partIndex === 0) {
                // First part - look for block definitions
                for (const symbol of symbols) {
                    if (symbol.name === parts[0]) {
                        return symbol.location;
                    }
                }
            }
            else {
                // Later parts - try to resolve the path
                // For now, just look for the component name
                const componentName = parts[partIndex];
                for (const symbol of symbols) {
                    if (symbol.name === componentName) {
                        return symbol.location;
                    }
                    // Also check if this is a component of a dot notation path
                    if (symbol.name.includes('.') && symbol.name.endsWith('.' + componentName)) {
                        return symbol.location;
                    }
                }
            }
        }
    }
    // Then check for include file references
    const includes = includeTable.get(uri) || [];
    for (const include of includes) {
        if (isPositionInRange(position, include.location.range)) {
            // Check if the file exists
            if (fs.existsSync(include.filePath)) {
                // Create a location for the file
                // Create a location for the file using proper URI encoding
                const fileUri = `file://${encodeURIComponent(include.filePath).replace(/%3A/g, ':').replace(/%2F/g, '/')}`;
                return node_1.Location.create(fileUri, node_1.Range.create(0, 0, 0, 0));
            }
        }
    }
    return null;
});
// Helper function to check if a position is within a range
function isPositionInRange(position, range) {
    // Check if position is after start and before end
    if (position.line < range.start.line || position.line > range.end.line) {
        return false;
    }
    if (position.line === range.start.line && position.character < range.start.character) {
        return false;
    }
    if (position.line === range.end.line && position.character > range.end.character) {
        return false;
    }
    return true;
}
// Handle find references requests
connection.onReferences((params) => {
    const uri = params.textDocument.uri;
    const position = params.position;
    // Get symbols for this document
    const symbols = symbolTable.get(uri) || [];
    // Find the symbol at the requested position
    let targetSymbol = null;
    for (const symbol of symbols) {
        if (isPositionInRange(position, symbol.location.range)) {
            targetSymbol = symbol;
            break;
        }
    }
    // If we didn't find a symbol at the exact position, try to find a word at the cursor
    if (!targetSymbol) {
        const document = documents.get(uri);
        if (document) {
            const text = document.getText();
            // Find the word at the cursor position
            const offset = document.offsetAt(position);
            let start = offset;
            let end = offset;
            // Move backward to find the start of the word
            while (start > 0 && /[a-zA-Z0-9_\-]/.test(text.charAt(start - 1))) {
                start--;
            }
            // Move forward to find the end of the word
            while (end < text.length && /[a-zA-Z0-9_\-]/.test(text.charAt(end))) {
                end++;
            }
            const word = text.substring(start, end);
            // Look for symbols that match this word
            for (const symbol of symbols) {
                if (symbol.name === word) {
                    targetSymbol = symbol;
                    break;
                }
                // Also check if this is a component of a dot notation path
                if (symbol.name.includes('.') && symbol.name.endsWith('.' + word)) {
                    targetSymbol = symbol;
                    break;
                }
                // Check if this is a component at the beginning of a dot notation path
                if (symbol.name.includes('.') && symbol.name.startsWith(word + '.')) {
                    targetSymbol = symbol;
                    break;
                }
            }
        }
    }
    if (!targetSymbol) {
        return [];
    }
    // Find all references to this symbol in all documents
    const references = [];
    for (const [docUri, docSymbols] of symbolTable.entries()) {
        for (const symbol of docSymbols) {
            if (symbol.name === targetSymbol.name) {
                references.push(symbol.location);
            }
            // Also add references where this symbol is part of a dot notation path
            if (symbol.name.includes(targetSymbol.name)) {
                references.push(symbol.location);
            }
        }
    }
    return references;
});
// Make the text document manager listen on the connection
// for open, change and close text document events
documents.listen(connection);
// Listen on the connection
connection.listen();
//# sourceMappingURL=server.js.map