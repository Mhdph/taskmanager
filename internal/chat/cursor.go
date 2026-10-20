package chat

import (
	"encoding/base64"
	"fmt"
	"strconv"
)

// Message cursors are opaque to clients: they carry the seq of the oldest
// message in the current page. Passing one back as "before" loads the
// previous page. seq is used instead of timestamps so paging cannot skip
// or repeat messages when several share a timestamp.

func EncodeMessageCursor(
	seq int64,
) string {
	return base64.RawURLEncoding.
		EncodeToString(
			[]byte(strconv.FormatInt(seq, 10)),
		)
}

func DecodeMessageCursor(
	value string,
) (int64, error) {
	decoded, err := base64.RawURLEncoding.
		DecodeString(value)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: %v",
			ErrInvalidCursor,
			err,
		)
	}

	seq, err := strconv.ParseInt(
		string(decoded),
		10,
		64,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: %v",
			ErrInvalidCursor,
			err,
		)
	}

	if seq < 0 {
		return 0, ErrInvalidCursor
	}

	return seq, nil
}
