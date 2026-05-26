package condition

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/oarkflow/condition/pkg/storage"
)

func TestConditionExamplesValidateStrict(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != "decision.bcl" {
			return nil
		}
		t.Run(filepath.Dir(path), func(t *testing.T) {
			svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true})
			report, err := svc.Validate(context.Background(), ValidationRequest{Name: filepath.Base(filepath.Dir(path)), Path: path, Strict: true})
			if err != nil {
				t.Fatalf("validate failed: %v\n%#v", err, report)
			}
			if report == nil || !report.Valid {
				t.Fatalf("invalid example: %#v", report)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
