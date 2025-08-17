package bcl

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
)

// ConcurrentParser enables parallel parsing of BCL files
type ConcurrentParser struct {
	workers int
	cache   *IncludeCache
}

// NewConcurrentParser creates a new concurrent parser
func NewConcurrentParser(workers int) *ConcurrentParser {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	return &ConcurrentParser{
		workers: workers,
		cache:   NewIncludeCache(),
	}
}

// ParseResult represents the result of parsing a file
type ParseResult struct {
	Filename string
	Nodes    []Node
	Env      *Environment
	Error    error
}

// ParseFiles parses multiple BCL files concurrently
func (cp *ConcurrentParser) ParseFiles(ctx context.Context, files []string) ([]ParseResult, error) {
	if len(files) == 0 {
		return nil, nil
	}

	// Create channels for work distribution
	fileChan := make(chan string, len(files))
	resultChan := make(chan ParseResult, len(files))

	// Create wait group for workers
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < cp.workers && i < len(files); i++ {
		wg.Add(1)
		go cp.worker(ctx, &wg, fileChan, resultChan)
	}

	// Send files to workers
	for _, file := range files {
		select {
		case fileChan <- file:
		case <-ctx.Done():
			close(fileChan)
			return nil, ctx.Err()
		}
	}
	close(fileChan)

	// Wait for all workers to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := make([]ParseResult, 0, len(files))
	for result := range resultChan {
		results = append(results, result)
	}

	return results, nil
}

// worker processes files from the file channel
func (cp *ConcurrentParser) worker(ctx context.Context, wg *sync.WaitGroup, files <-chan string, results chan<- ParseResult) {
	defer wg.Done()

	for file := range files {
		select {
		case <-ctx.Done():
			return
		default:
			result := cp.parseFile(file)
			results <- result
		}
	}
}

// parseFile parses a single BCL file
func (cp *ConcurrentParser) parseFile(filename string) ParseResult {
	result := ParseResult{Filename: filename}

	// Read file content
	content, err := readFileContent(filename)
	if err != nil {
		result.Error = fmt.Errorf("failed to read file %s: %w", filename, err)
		return result
	}

	// Parse the content
	parser := NewParser(string(content))
	nodes, err := parser.Parse()
	if err != nil {
		result.Error = fmt.Errorf("failed to parse file %s: %w", filename, err)
		return result
	}

	// Evaluate nodes
	env := NewEnv(nil)
	for _, node := range nodes {
		if _, err := node.Eval(env); err != nil {
			result.Error = fmt.Errorf("failed to evaluate node in file %s: %w", filename, err)
			return result
		}
	}

	result.Nodes = nodes
	result.Env = env
	return result
}

// ParseAndMerge parses multiple files and merges their environments
func (cp *ConcurrentParser) ParseAndMerge(ctx context.Context, files []string) (*Environment, error) {
	results, err := cp.ParseFiles(ctx, files)
	if err != nil {
		return nil, err
	}

	// Check for errors
	var errors MultiError
	for _, result := range results {
		if result.Error != nil {
			errors.Add(result.Error)
		}
	}
	if errors.HasErrors() {
		return nil, &errors
	}

	// Merge environments
	merged := NewEnv(nil)
	for _, result := range results {
		for k, v := range result.Env.vars {
			merged.vars[k] = v
		}
	}

	return merged, nil
}

// BatchEvaluator evaluates multiple expressions concurrently
type BatchEvaluator struct {
	workers int
}

// NewBatchEvaluator creates a new batch evaluator
func NewBatchEvaluator(workers int) *BatchEvaluator {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	return &BatchEvaluator{workers: workers}
}

// EvalResult represents the result of evaluating an expression
type EvalResult struct {
	Index int
	Value interface{}
	Error error
}

// EvaluateExpressions evaluates multiple expressions concurrently
func (be *BatchEvaluator) EvaluateExpressions(ctx context.Context, expressions []string, env *Environment) ([]interface{}, error) {
	if len(expressions) == 0 {
		return nil, nil
	}

	// Create channels
	workChan := make(chan struct {
		index int
		expr  string
	}, len(expressions))
	resultChan := make(chan EvalResult, len(expressions))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < be.workers && i < len(expressions); i++ {
		wg.Add(1)
		go be.evalWorker(ctx, &wg, workChan, resultChan, env)
	}

	// Send work
	for i, expr := range expressions {
		select {
		case workChan <- struct {
			index int
			expr  string
		}{i, expr}:
		case <-ctx.Done():
			close(workChan)
			return nil, ctx.Err()
		}
	}
	close(workChan)

	// Wait for completion
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := make([]interface{}, len(expressions))
	var errors MultiError

	for result := range resultChan {
		if result.Error != nil {
			errors.Add(fmt.Errorf("expression %d: %w", result.Index, result.Error))
		} else {
			results[result.Index] = result.Value
		}
	}

	if errors.HasErrors() {
		return nil, &errors
	}

	return results, nil
}

// evalWorker processes expressions
func (be *BatchEvaluator) evalWorker(ctx context.Context, wg *sync.WaitGroup, work <-chan struct {
	index int
	expr  string
}, results chan<- EvalResult, env *Environment) {
	defer wg.Done()

	for w := range work {
		select {
		case <-ctx.Done():
			return
		default:
			parser := NewParser(w.expr)
			node, err := parser.parseExpression()
			if err != nil {
				results <- EvalResult{Index: w.index, Error: err}
				continue
			}

			value, err := node.Eval(env)
			results <- EvalResult{
				Index: w.index,
				Value: value,
				Error: err,
			}
		}
	}
}

// readFileContent reads file content (helper function)
func readFileContent(filename string) ([]byte, error) {
	// This is a simplified version - in production, you might want to
	// handle URLs, relative paths, etc. similar to the include logic
	return os.ReadFile(filename)
}
