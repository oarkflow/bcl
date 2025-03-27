package utils

import (
	"fmt"
	"os"
	"sync"
	"unsafe"
)

func B2S(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

func S2B(s string) []byte {
	return *(*[]byte)(unsafe.Pointer(&s))
}

// ToString - Basic type conversion functions
func ToString(val any) (string, bool) {
	switch v := val.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	case fmt.Stringer:
		return v.String(), true
	default:
		return fmt.Sprintf("%v", val), true
	}
}

type GetEnvFn func(v string, defaultVal ...any) string

var Getenv GetEnvFn

func getenv(v string, defaultVal ...any) string {
	val := os.Getenv(v)
	if val != "" {
		return val
	}
	if len(defaultVal) > 0 && defaultVal[0] != nil {
		val, _ := ToString(defaultVal[0])
		return val
	}
	return ""
}

func init() {
	Getenv = getenv
}

type Env struct {
	Store map[string]any
	outer *Env
	m     sync.RWMutex
}

func NewEnv() *Env {
	return &Env{Store: make(map[string]any)}
}

func (e *Env) Get(name string) (any, bool) {
	e.m.Lock()
	defer e.m.Unlock()
	if val, ok := e.Store[name]; ok {
		return val, true
	}
	if e.outer != nil {
		return e.outer.Get(name)
	}
	return nil, false
}

func (e *Env) Set(name string, val any) {
	e.m.Lock()
	defer e.m.Unlock()
	e.Store[name] = val
}

func (e *Env) Extend() *Env {
	return &Env{Store: make(map[string]any), outer: e}
}
