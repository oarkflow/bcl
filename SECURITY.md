# Security

## Reporting a vulnerability

Report suspected vulnerabilities privately to the maintainers rather than in a
public issue. Include a document or program that reproduces the problem and the
version or commit you tested.

## The trust boundary

**A BCL document is code, not data.** Compiling and evaluating one can read
files, reach the network, and run `git`. Treat a document the way you treat a
Go file in your repository: something an author you trust wrote and a reviewer
read. Do not compile documents submitted by end users without the restrictions
below.

What a document can reach, and what stops it:

| Reach | Controlled by | Default |
| --- | --- | --- |
| `import` and module directories | `Options.BaseDir`, `Options.ResolveImports` | Imports are read relative to the document; resolution is off unless requested |
| Environment variables via `env()` | `Options.AllowEnv` | Denied |
| Wall-clock time via `now()`, `CURRENT_TIMESTAMP` | `Options.AllowTime` | Denied |
| `sha256()` / `md5()` hashing | `Options.AllowHash` | Denied |
| `base64()` encoding | `Options.AllowEncoding` | Denied |
| `file` datasets | `Options.AllowedDatasetRoots` | Confined to the directory of the document that declared the dataset; absolute paths, `../` escapes and symlinks out of that directory are refused |
| `http` / `https` datasets | `Options.AllowedHTTPHosts`, `Options.AllowedHTTPMethods` | **Denied.** Network access requires an explicit host list; `"*"` allows any host |
| Dataset adapters generally | `Options.AllowedDatasetAdapters` | Any registered adapter; set the list to restrict |
| Outbound request duration | `Options.ExternalTimeout` | `DefaultExternalTimeout` (30s); never unbounded |
| Module fetching (`git clone`, registry archives) | `FetchOptions.AllowedHosts`, `FetchOptions.Timeout` | Any host, `DefaultFetchTimeout` (2m). Sources that git would read as an option, and the `ext::` and `fd::` transports which execute commands, are always refused |

Expressions themselves cannot reach outside the process: there is no builtin that
reads a file, opens a socket, or runs a command. Everything in the table above
goes through a declared block that the embedder's `Options` govern.

### Evaluating documents you do not control

Set every capability explicitly:

```go
opts := &bcl.Options{
    BaseDir:             sandboxDir,
    AllowEnv:            false,
    AllowTime:           false,
    AllowHash:           false,
    AllowEncoding:       false,
    AllowedDatasetRoots: []string{sandboxDir},
    AllowedHTTPHosts:    nil,                 // no network
    ExternalTimeout:     5 * time.Second,
    Strict:              true,
}
```

Also bound the input itself. Parsing recurses once per nesting level, so
documents are refused beyond `bcl.MaxNestingDepth` (512) — without that bound a
large enough file exhausts the goroutine stack and ends the process. Apply your
own limit on document size before parsing.

## What is not a vulnerability

- A document reading a file beside itself, or one under a directory you listed
  in `AllowedDatasetRoots`. That is the declared contract.
- A document reaching a host you listed in `AllowedHTTPHosts`.
- Slow evaluation of a document that declares expensive work. Use
  `Options.ExternalTimeout` and your own deadline.

## Hardening history

- Dataset file access is confined to the declaring document's directory,
  including through symlinks.
- HTTP datasets require an explicit host allowlist.
- Every outbound request carries a deadline; `http.DefaultClient`, which has
  none, is no longer used.
- Registry responses and module archives are size-capped.
- `git clone` refuses sources that begin with `-` (git reads them as options, and
  `--upload-pack=<cmd>` executes `<cmd>`) and the `ext::` and `fd::` transports.
  Revisions must look like refs. Git runs with prompting disabled and a deadline.
- Parsing enforces `MaxNestingDepth`, turning a fatal stack overflow into a
  diagnostic.
