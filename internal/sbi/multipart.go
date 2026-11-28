package sbi

import (
	"io"
	"mime/multipart"
)

// newMultipartReader isolates mime/multipart construction so tests can inject
// malformed boundaries through the same path.
func newMultipartReader(body io.Reader, boundary string) *multipart.Reader {
	return multipart.NewReader(body, boundary)
}
