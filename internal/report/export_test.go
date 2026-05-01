package report

import "io"

// SetReporterFields is exported for tests only (export_test.go lives in package
// report so the symbol is visible to report_test through *_test compilation).
func SetReporterFields(r *SARIFReporter, w io.Writer, version string) {
	r.w = w
	r.opts.Version = version
}

func SetJSONReporterFields(r *JSONReporter, w io.Writer, version string) {
	r.w = w
	r.opts.Version = version
}

func SetHumanReporterFields(r *HumanReporter, w io.Writer, version string, noColor bool) {
	r.w = w
	r.opts.Version = version
	r.opts.NoColor = noColor
}
