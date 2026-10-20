package chat

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestSanitizeAttachmentFilename(t *testing.T) {
	cases := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "report.pdf", want: "report.pdf", ok: true},
		{input: "  report.pdf  ", want: "report.pdf", ok: true},
		{input: "../../etc/passwd", want: "passwd", ok: true},
		{input: `..\windows\system32\evil.dll`, want: "evil.dll", ok: true},
		{input: `say "hi".txt`, want: "say hi.txt", ok: true},
		{input: "a\x00b.txt", want: "ab.txt", ok: true},
		{input: "", ok: false},
		{input: "..", ok: false},
		{input: "/", ok: false},
	}

	for _, tc := range cases {
		got, err := sanitizeAttachmentFilename(tc.input)

		if tc.ok {
			if err != nil {
				t.Fatalf("%q: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("%q = %q, want %q", tc.input, got, tc.want)
			}

			continue
		}

		if !errors.Is(err, ErrInvalidAttachmentFilename) {
			t.Fatalf("%q error = %v", tc.input, err)
		}
	}
}

func TestNormalizeAttachmentIDs(t *testing.T) {
	first := uuid.NewString()
	second := uuid.NewString()

	ids, err := normalizeAttachmentIDs([]string{
		first,
		" " + first + " ",
		second,
		"",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatalf("ids = %v", ids)
	}

	if _, err := normalizeAttachmentIDs([]string{"not-a-uuid"}); !errors.Is(
		err,
		ErrInvalidAttachment,
	) {
		t.Fatalf("invalid id error = %v", err)
	}

	tooMany := make([]string, 0, maxAttachmentsPerMessage+1)
	for i := 0; i <= maxAttachmentsPerMessage; i++ {
		tooMany = append(tooMany, uuid.NewString())
	}

	if _, err := normalizeAttachmentIDs(tooMany); !errors.Is(
		err,
		ErrTooManyAttachments,
	) {
		t.Fatalf("too many error = %v", err)
	}
}

func TestNormalizeAttachmentContentType(t *testing.T) {
	if got := normalizeAttachmentContentType(" image/png "); got != "image/png" {
		t.Fatalf("content type = %q", got)
	}

	if got := normalizeAttachmentContentType(""); got != "application/octet-stream" {
		t.Fatalf("empty content type = %q", got)
	}

	if got := normalizeAttachmentContentType("bad\r\nvalue"); got != "badvalue" {
		t.Fatalf("control chars = %q", got)
	}
}
