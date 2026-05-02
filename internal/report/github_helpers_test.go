package report_test

import (
	"os"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
)

func stubResult(fs ...findings.Finding) *engine.Result {
	res := &engine.Result{
		FilesScanned:   1,
		CommitsScanned: 0,
		DurationMS:     0,
		Findings:       fs,
	}
	res.Summary = findings.Summarize(res.Findings)
	return res
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
