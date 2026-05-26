package bcl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DecisionDatasetAdapter interface {
	OpenDataset(ctx context.Context, source DatasetSource, opts *Options) (DecisionRecordIterator, error)
}

type DecisionDatasetAdapterFunc func(ctx context.Context, source DatasetSource, opts *Options) (DecisionRecordIterator, error)

func (f DecisionDatasetAdapterFunc) OpenDataset(ctx context.Context, source DatasetSource, opts *Options) (DecisionRecordIterator, error) {
	return f(ctx, source, opts)
}

type DecisionRecordIterator interface {
	Next(ctx context.Context) (DecisionCandidate, bool, error)
	Close() error
}

var decisionDatasetAdapters = struct {
	sync.RWMutex
	m map[string]DecisionDatasetAdapter
}{m: map[string]DecisionDatasetAdapter{}}

func RegisterDecisionDatasetAdapter(kind string, adapter DecisionDatasetAdapter) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" || adapter == nil {
		return
	}
	decisionDatasetAdapters.Lock()
	defer decisionDatasetAdapters.Unlock()
	decisionDatasetAdapters.m[kind] = adapter
}

func OpenDecisionDataset(ctx context.Context, program *DecisionProgram, datasetID string, opts *Options) (DecisionRecordIterator, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	dataset := program.Datasets[datasetID]
	if dataset == nil {
		return nil, fmt.Errorf("unknown dataset %q", datasetID)
	}
	if dataset.Source.Adapter == "" || strings.EqualFold(dataset.Source.Adapter, "inline") {
		return &sliceDecisionIterator{records: dataset.Records}, nil
	}
	if !datasetAdapterAllowed(dataset.Source.Adapter, opts) {
		return nil, fmt.Errorf("dataset adapter %q is not allowed", dataset.Source.Adapter)
	}
	adapter, ok := decisionDatasetAdapterFor(dataset.Source.Adapter, opts)
	if !ok {
		return nil, fmt.Errorf("unknown dataset adapter %q", dataset.Source.Adapter)
	}
	return adapter.OpenDataset(ctx, dataset.Source, opts)
}

func decisionDatasetAdapterFor(kind string, opts *Options) (DecisionDatasetAdapter, bool) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if opts != nil && opts.DecisionDatasetAdapters != nil {
		if adapter := opts.DecisionDatasetAdapters[kind]; adapter != nil {
			return adapter, true
		}
	}
	decisionDatasetAdapters.RLock()
	adapter := decisionDatasetAdapters.m[kind]
	decisionDatasetAdapters.RUnlock()
	if adapter != nil {
		return adapter, true
	}
	switch kind {
	case "inline":
		return DecisionDatasetAdapterFunc(func(_ context.Context, _ DatasetSource, _ *Options) (DecisionRecordIterator, error) {
			return &sliceDecisionIterator{}, nil
		}), true
	case "file":
		return DecisionDatasetAdapterFunc(openFileDecisionDataset), true
	case "http", "https":
		return DecisionDatasetAdapterFunc(openHTTPDecisionDataset), true
	default:
		return nil, false
	}
}

type sliceDecisionIterator struct {
	records []DecisionCandidate
	idx     int
}

func (it *sliceDecisionIterator) Next(ctx context.Context) (DecisionCandidate, bool, error) {
	if err := ctx.Err(); err != nil {
		return DecisionCandidate{}, false, err
	}
	if it.idx >= len(it.records) {
		return DecisionCandidate{}, false, nil
	}
	record := it.records[it.idx]
	it.idx++
	return record, true, nil
}

func (it *sliceDecisionIterator) Close() error { return nil }

func openFileDecisionDataset(ctx context.Context, source DatasetSource, opts *Options) (DecisionRecordIterator, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := scalarString(source.Config["path"])
	if path == "" {
		path = scalarString(source.Config["url"])
	}
	if path == "" {
		return nil, fmt.Errorf("file dataset source requires path")
	}
	if opts != nil && opts.BaseDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(opts.BaseDir, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	format := strings.ToLower(firstNonEmpty(scalarString(source.Config["format"]), strings.TrimPrefix(filepath.Ext(path), ".")))
	switch format {
	case "jsonl", "ndjson":
		return newJSONLDecisionIterator(f, source), nil
	case "csv":
		return newCSVDecisionIterator(f, source)
	case "json", "":
		return newJSONDecisionIterator(f, source)
	default:
		_ = f.Close()
		return nil, fmt.Errorf("unsupported file dataset format %q", format)
	}
}

func openHTTPDecisionDataset(ctx context.Context, source DatasetSource, opts *Options) (DecisionRecordIterator, error) {
	url := firstNonEmpty(scalarString(source.Config["url"]), scalarString(source.Config["endpoint"]), scalarString(source.Config["base_url"]))
	if url == "" {
		return nil, fmt.Errorf("http dataset source requires url")
	}
	method := strings.ToUpper(firstNonEmpty(scalarString(source.Config["method"]), "GET"))
	if err := validateHTTPDatasetPolicy(url, method, opts); err != nil {
		return nil, err
	}
	var body io.Reader
	if payload := source.Config["body"]; payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range stringMap(source.Config["headers"]) {
		req.Header.Set(k, v)
	}
	client := http.DefaultClient
	if opts != nil && opts.HTTPClient != nil {
		client = opts.HTTPClient
	} else if opts != nil && opts.ExternalTimeout > 0 {
		client = &http.Client{Timeout: opts.ExternalTimeout}
	} else if timeout := durationValue(source.Config["timeout"]); timeout > 0 {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if !statusAllowed(resp.StatusCode, intListAny(source.Config["expect_status"])) {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http dataset source returned %s", resp.Status)
	}
	format := strings.ToLower(firstNonEmpty(scalarString(source.Config["format"]), "json"))
	if responsePath := scalarString(firstNonNil(source.Config["response_path"], source.Config["records_path"])); responsePath != "" {
		defer resp.Body.Close()
		var payload any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return nil, err
		}
		return &sliceDecisionIterator{records: candidatesFromAny(lookupAny(payload, responsePath), source)}, nil
	}
	switch format {
	case "jsonl", "ndjson":
		return newJSONLDecisionIterator(resp.Body, source), nil
	case "json", "":
		return newJSONDecisionIterator(resp.Body, source)
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unsupported http dataset format %q", format)
	}
}

func datasetAdapterAllowed(adapter string, opts *Options) bool {
	if opts == nil || len(opts.AllowedDatasetAdapters) == 0 {
		return true
	}
	adapter = strings.ToLower(strings.TrimSpace(adapter))
	for _, allowed := range opts.AllowedDatasetAdapters {
		if strings.EqualFold(strings.TrimSpace(allowed), adapter) {
			return true
		}
	}
	return false
}

func validateHTTPDatasetPolicy(rawURL, method string, opts *Options) error {
	if opts == nil {
		return nil
	}
	if len(opts.AllowedHTTPMethods) > 0 {
		allowed := false
		for _, candidate := range opts.AllowedHTTPMethods {
			if strings.EqualFold(strings.TrimSpace(candidate), method) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("http dataset method %q is not allowed", method)
		}
	}
	if len(opts.AllowedHTTPHosts) == 0 {
		return nil
	}
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return err
	}
	host := strings.ToLower(req.URL.Hostname())
	for _, allowed := range opts.AllowedHTTPHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return nil
		}
	}
	return fmt.Errorf("http dataset host %q is not allowed", host)
}

type closeReader interface {
	io.Reader
	io.Closer
}

type jsonArrayDecisionIterator struct {
	closer io.Closer
	dec    *json.Decoder
	source DatasetSource
	done   bool
}

func newJSONDecisionIterator(r closeReader, source DatasetSource) (DecisionRecordIterator, error) {
	br := bufio.NewReader(r)
	for {
		b, err := br.Peek(1)
		if err != nil {
			_ = r.Close()
			return nil, err
		}
		if b[0] != ' ' && b[0] != '\n' && b[0] != '\r' && b[0] != '\t' {
			break
		}
		_, _ = br.ReadByte()
	}
	if b, _ := br.Peek(1); len(b) == 1 && b[0] == '[' {
		dec := json.NewDecoder(br)
		if _, err := dec.Token(); err != nil {
			_ = r.Close()
			return nil, err
		}
		return &jsonArrayDecisionIterator{closer: r, dec: dec, source: source}, nil
	}
	defer r.Close()
	var payload any
	if err := json.NewDecoder(br).Decode(&payload); err != nil {
		return nil, err
	}
	return &sliceDecisionIterator{records: candidatesFromAny(payload, source)}, nil
}

func (it *jsonArrayDecisionIterator) Next(ctx context.Context) (DecisionCandidate, bool, error) {
	if err := ctx.Err(); err != nil {
		return DecisionCandidate{}, false, err
	}
	if it.done || !it.dec.More() {
		it.done = true
		return DecisionCandidate{}, false, nil
	}
	var item any
	if err := it.dec.Decode(&item); err != nil {
		return DecisionCandidate{}, false, err
	}
	return candidateFromAny(item, it.source), true, nil
}

func (it *jsonArrayDecisionIterator) Close() error {
	if it.closer == nil {
		return nil
	}
	return it.closer.Close()
}

type jsonlDecisionIterator struct {
	closer  io.Closer
	scanner *bufio.Scanner
	source  DatasetSource
}

func newJSONLDecisionIterator(r closeReader, source DatasetSource) DecisionRecordIterator {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	return &jsonlDecisionIterator{closer: r, scanner: scanner, source: source}
}

func (it *jsonlDecisionIterator) Next(ctx context.Context) (DecisionCandidate, bool, error) {
	if err := ctx.Err(); err != nil {
		return DecisionCandidate{}, false, err
	}
	for it.scanner.Scan() {
		line := bytes.TrimSpace(it.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var item any
		if err := json.Unmarshal(line, &item); err != nil {
			return DecisionCandidate{}, false, err
		}
		return candidateFromAny(item, it.source), true, nil
	}
	if err := it.scanner.Err(); err != nil {
		return DecisionCandidate{}, false, err
	}
	return DecisionCandidate{}, false, nil
}

func (it *jsonlDecisionIterator) Close() error { return it.closer.Close() }

type csvDecisionIterator struct {
	closer  io.Closer
	reader  *csv.Reader
	headers []string
	source  DatasetSource
}

func newCSVDecisionIterator(r closeReader, source DatasetSource) (DecisionRecordIterator, error) {
	reader := csv.NewReader(r)
	headers, err := reader.Read()
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	return &csvDecisionIterator{closer: r, reader: reader, headers: headers, source: source}, nil
}

func (it *csvDecisionIterator) Next(ctx context.Context) (DecisionCandidate, bool, error) {
	if err := ctx.Err(); err != nil {
		return DecisionCandidate{}, false, err
	}
	row, err := it.reader.Read()
	if err == io.EOF {
		return DecisionCandidate{}, false, nil
	}
	if err != nil {
		return DecisionCandidate{}, false, err
	}
	m := map[string]any{}
	for i, header := range it.headers {
		if i < len(row) {
			m[header] = parseCSVScalar(row[i])
		}
	}
	return candidateFromMap(m, it.source), true, nil
}

func (it *csvDecisionIterator) Close() error { return it.closer.Close() }

func candidatesFromAny(v any, source DatasetSource) []DecisionCandidate {
	items := asAnySlice(v)
	out := make([]DecisionCandidate, 0, len(items))
	for _, item := range items {
		out = append(out, candidateFromAny(item, source))
	}
	return out
}

func candidateFromAny(v any, source DatasetSource) DecisionCandidate {
	if m, ok := v.(map[string]any); ok {
		return candidateFromMap(m, source)
	}
	return DecisionCandidate{Facts: map[string]any{"value": v}}
}

func candidateFromMap(m map[string]any, source DatasetSource) DecisionCandidate {
	idPath := firstNonEmpty(scalarString(source.Config["id_path"]), "id")
	factsPath := scalarString(source.Config["facts_path"])
	id := scalarString(lookupAny(m, idPath))
	var facts map[string]any
	if factsPath != "" {
		facts, _ = lookupAny(m, factsPath).(map[string]any)
	} else if nested, ok := m["facts"].(map[string]any); ok {
		facts = nested
	} else if nested, ok := m["input"].(map[string]any); ok {
		facts = nested
	}
	if facts == nil {
		facts = map[string]any{}
		for k, v := range m {
			if k != idPath {
				facts[k] = v
			}
		}
	}
	return DecisionCandidate{ID: id, Facts: facts}
}

func parseCSVScalar(s string) any {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}
	return s
}

func lookupAny(v any, path string) any {
	if path == "" {
		return v
	}
	if m, ok := v.(map[string]any); ok {
		return lookup(m, path)
	}
	return nil
}

func stringMap(v any) map[string]string {
	out := map[string]string{}
	for k, value := range decisionMapValue(v) {
		out[k] = scalarString(value)
	}
	return out
}

func intListAny(v any) []int {
	var out []int
	for _, item := range asAnySlice(v) {
		if n := intValue(item); n != 0 {
			out = append(out, int(n))
		}
	}
	return out
}

func statusAllowed(status int, allowed []int) bool {
	if len(allowed) == 0 {
		return status >= 200 && status < 300
	}
	for _, code := range allowed {
		if status == code {
			return true
		}
	}
	return false
}

func durationValue(v any) time.Duration {
	switch x := v.(type) {
	case time.Duration:
		return x
	case int64:
		return time.Duration(x) * time.Second
	case int:
		return time.Duration(x) * time.Second
	case float64:
		return time.Duration(x * float64(time.Second))
	case string:
		d, _ := time.ParseDuration(x)
		return d
	default:
		return 0
	}
}

func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}
