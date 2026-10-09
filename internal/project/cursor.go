package project

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

type Cursor struct {
	CreatedAt time.Time
	ID        string
}

func EncodeCursor(cursor Cursor) string {
	value := cursor.CreatedAt.UTC().Format(time.RFC3339Nano) +
		"|" +
		cursor.ID

	return base64.RawURLEncoding.EncodeToString(
		[]byte(value),
	)
}

func DecodeCursor(value string) (Cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, fmt.Errorf(
			"decode cursor: %w",
			err,
		)
	}

	parts := strings.SplitN(
		string(decoded),
		"|",
		2,
	)

	if len(parts) != 2 {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}

	createdAt, err := time.Parse(
		time.RFC3339Nano,
		parts[0],
	)
	if err != nil {
		return Cursor{}, fmt.Errorf(
			"parse cursor time: %w",
			err,
		)
	}

	if parts[1] == "" {
		return Cursor{}, fmt.Errorf(
			"cursor id is empty",
		)
	}

	return Cursor{
		CreatedAt: createdAt,
		ID:        parts[1],
	}, nil
}
