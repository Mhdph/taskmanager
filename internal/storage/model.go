package storage

import "io"

type UploadInput struct {
	ObjectKey string

	Reader io.Reader

	Size int64

	ContentType string
}
