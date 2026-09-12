package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oarkflow/bcl"
)

func newIndexedServer(tb testing.TB) (*server, string) {
	tb.Helper()
	root, _ := os.Getwd()
	root = root + "/../.."
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(root)}
	s.indexWorkspace()
	uri := pathURI(root + "/example/app.bcl")
	b, err := os.ReadFile(uriPath(uri))
	if err != nil {
		tb.Fatal(err)
	}
	s.files[uri] = string(b)
	return s, uri
}

func BenchmarkIndexWorkspace(b *testing.B) {
	root, _ := os.Getwd()
	root = root + "/../.."
	for b.Loop() {
		s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(root)}
		s.indexWorkspace()
	}
}

func BenchmarkKeystrokeSetFile(b *testing.B) {
	s, uri := newIndexedServer(b)
	text := s.files[uri]
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		s.setFile(uri, text+"\n")
	}
}

func BenchmarkAnalyzeURIOnly(b *testing.B) {
	s, uri := newIndexedServer(b)
	b.ReportAllocs()
	for b.Loop() {
		s.analyzeURI(uri)
	}
}

func BenchmarkReanalyzeDependentsOnly(b *testing.B) {
	s, uri := newIndexedServer(b)
	b.ReportAllocs()
	for b.Loop() {
		s.reanalyzeDependents(uri)
	}
}

func BenchmarkOwnerEntrypoint(b *testing.B) {
	s, uri := newIndexedServer(b)
	path := uriPath(uri)
	b.ReportAllocs()
	for b.Loop() {
		s.ownerEntrypoint(path)
	}
}

func BenchmarkKeystrokeOnLargePipeline(b *testing.B) {
	root, _ := os.Getwd()
	root = root + "/../.."
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(root)}
	s.indexWorkspace()
	b.Logf("indexed files: %d", len(s.index))
	fixture := filepath.Join(root, "testdata", "large_pipeline.bcl")
	src, err := os.ReadFile(fixture)
	if err != nil {
		b.Fatal(err)
	}
	uri := pathURI(fixture)
	s.files[uri] = string(src)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		s.setFile(uri, string(src)+"\n")
	}
}
