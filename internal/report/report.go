package report

import (
	"fmt"
	"io"
	"os"

	"github.com/JadenRazo/llm-lint/internal/engine"
)

type Format string

const (
	FormatHuman Format = "human"
	FormatJSON  Format = "json"
	FormatSARIF Format = "sarif"
)

type Options struct {
	NoColor bool
	Output  string
	Version string
}

type Reporter interface {
	Write(res *engine.Result) error
}

func New(format string, opts Options) (Reporter, error) {
	w, closer, err := openOutput(opts.Output)
	if err != nil {
		return nil, err
	}
	switch Format(format) {
	case FormatHuman, "":
		return &HumanReporter{w: w, closer: closer, opts: opts}, nil
	case FormatJSON:
		return &JSONReporter{w: w, closer: closer, opts: opts}, nil
	case FormatSARIF:
		return &SARIFReporter{w: w, closer: closer, opts: opts}, nil
	default:
		if closer != nil {
			closer.Close()
		}
		return nil, fmt.Errorf("unknown format %q (want human|json|sarif)", format)
	}
}

func openOutput(path string) (io.Writer, io.Closer, error) {
	if path == "" || path == "-" {
		return os.Stdout, nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f, nil
}
