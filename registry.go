package bcl

import (
	"fmt"
	"strings"
	"sync"
)

// FunctionRegistry manages registered functions in a thread-safe manner
type FunctionRegistry struct {
	mu    sync.RWMutex
	funcs map[string]Function
}

// NewFunctionRegistry creates a new function registry
func NewFunctionRegistry() *FunctionRegistry {
	return &FunctionRegistry{
		funcs: make(map[string]Function),
	}
}

// Register adds a function to the registry
func (r *FunctionRegistry) Register(name string, fn Function) error {
	if name == "" {
		return fmt.Errorf("function name cannot be empty")
	}
	if fn == nil {
		return fmt.Errorf("function cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	normalizedName := strings.ToLower(name)
	if _, exists := r.funcs[normalizedName]; exists {
		return fmt.Errorf("function %s already registered", name)
	}

	r.funcs[normalizedName] = fn
	return nil
}

// Lookup retrieves a function from the registry
func (r *FunctionRegistry) Lookup(name string) (Function, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	fn, ok := r.funcs[strings.ToLower(name)]
	return fn, ok
}

// List returns all registered function names
func (r *FunctionRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.funcs))
	for name := range r.funcs {
		names = append(names, name)
	}
	return names
}

// Clear removes all registered functions
func (r *FunctionRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.funcs = make(map[string]Function)
}

// Global registry instance
var globalRegistry = NewFunctionRegistry()

// RegisterFunction registers a function in the global registry
func RegisterFunction(name string, fn Function) error {
	return globalRegistry.Register(name, fn)
}

// LookupFunction looks up a function in the global registry
func LookupFunction(name string) (Function, bool) {
	return globalRegistry.Lookup(name)
}

// ClearFunctions clears all functions from the global registry
func ClearFunctions() {
	globalRegistry.Clear()
}

// IncludeCache manages cached include files in a thread-safe manner
type IncludeCache struct {
	mu    sync.RWMutex
	cache map[string][]Node
}

// NewIncludeCache creates a new include cache
func NewIncludeCache() *IncludeCache {
	return &IncludeCache{
		cache: make(map[string][]Node),
	}
}

// Get retrieves nodes from the cache
func (c *IncludeCache) Get(filename string) ([]Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodes, ok := c.cache[filename]
	return nodes, ok
}

// Set stores nodes in the cache
func (c *IncludeCache) Set(filename string, nodes []Node) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[filename] = nodes
}

// Clear removes all cached includes
func (c *IncludeCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string][]Node)
}

// Global include cache instance
var globalIncludeCache = NewIncludeCache()
