package task

import (
	"testing"
	"time"
)

func TestWorkspaceCursorRoundTrip(t *testing.T) {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 3, 2, 8, 30, 0, 0, time.UTC)
	due := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

	title := "Some Task Title"
	priority := int32(3)

	cursor := WorkspaceCursor{
		Sort:      WorkspaceSortDue,
		Desc:      true,
		ID:        "0f6a4a2e-1111-4222-8333-444455556666",
		CreatedAt: &created,
		UpdatedAt: &updated,
		DueAt:     &due,
		Priority:  &priority,
		Title:     &title,
	}

	encoded, err := EncodeWorkspaceCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeWorkspaceCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Sort != cursor.Sort {
		t.Fatalf("sort = %q, want %q", decoded.Sort, cursor.Sort)
	}

	if decoded.Desc != cursor.Desc {
		t.Fatalf("desc = %v, want %v", decoded.Desc, cursor.Desc)
	}

	if decoded.ID != cursor.ID {
		t.Fatalf("id = %q, want %q", decoded.ID, cursor.ID)
	}

	if decoded.DueAt == nil || !decoded.DueAt.Equal(due) {
		t.Fatalf("due = %v, want %v", decoded.DueAt, due)
	}

	if decoded.CreatedAt == nil || !decoded.CreatedAt.Equal(created) {
		t.Fatalf("created mismatch")
	}

	if decoded.UpdatedAt == nil || !decoded.UpdatedAt.Equal(updated) {
		t.Fatalf("updated mismatch")
	}

	if decoded.Priority == nil || *decoded.Priority != priority {
		t.Fatalf("priority mismatch")
	}

	if decoded.Title == nil || *decoded.Title != title {
		t.Fatalf("title mismatch")
	}

	if decoded.DueIsNull {
		t.Fatal("dueIsNull should be false")
	}
}

func TestWorkspaceCursorRoundTripNullDue(t *testing.T) {
	cursor := WorkspaceCursor{
		Sort:      WorkspaceSortDue,
		Desc:      false,
		ID:        "0f6a4a2e-1111-4222-8333-444455556666",
		DueIsNull: true,
	}

	encoded, err := EncodeWorkspaceCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeWorkspaceCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if !decoded.DueIsNull || decoded.DueAt != nil {
		t.Fatal("null due marker lost in round trip")
	}
}

func TestDecodeWorkspaceCursorRejectsGarbage(t *testing.T) {
	tests := []string{
		"not-base64!!",
		"aGVsbG8=",   // decodes, but not JSON
		"eyJ4IjoxfQ", // {"x":1} — no sort/id
	}

	for _, raw := range tests {
		if _, err := DecodeWorkspaceCursor(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestWorkspaceCursorFromLastTaskDue(t *testing.T) {
	due := time.Now().UTC()

	cursor, err := workspaceCursorFromLastTask(
		WorkspaceSortDue,
		false,
		Task{ID: "abc", DueAt: &due},
	)
	if err != nil {
		t.Fatal(err)
	}

	if cursor.DueAt == nil || !cursor.DueAt.Equal(due) {
		t.Fatal("due value not captured")
	}

	cursor, err = workspaceCursorFromLastTask(
		WorkspaceSortDue,
		false,
		Task{ID: "abc"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !cursor.DueIsNull {
		t.Fatal("null due task must yield DueIsNull=true")
	}
}
